//go:build js

package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"syscall/js"

	"mistfall/v2/core"
)

//go:embed assets/database.json
var embeddedDatabase []byte

//go:embed assets/affixes.json
var embeddedAffixes []byte

var (
	engine    *core.Engine
	initError error
	callbacks []js.Func
)

func jsonValue(value any) js.Value {
	data, err := json.Marshal(value)
	if err != nil {
		return js.ValueOf(nil)
	}
	return js.Global().Get("JSON").Call("parse", string(data))
}

func initCore(_ js.Value, _ []js.Value) any {
	core.ConfigureAssets(embeddedDatabase, embeddedAffixes)
	engine, initError = core.NewEngine()
	if initError != nil {
		return initError.Error()
	}
	return nil
}

func getOptions(_ js.Value, _ []js.Value) any {
	if initError != nil {
		return initError.Error()
	}
	if engine == nil {
		return "core is not initialized"
	}
	return jsonValue(engine.Options())
}

func execute(_ js.Value, args []js.Value) any {
	if initError != nil {
		return js.Global().Get("Promise").Call("reject", js.ValueOf(initError.Error()))
	}
	if engine == nil {
		return js.Global().Get("Promise").Call("reject", js.ValueOf("core is not initialized"))
	}
	if len(args) != 1 {
		return js.Global().Get("Promise").Call("reject", js.ValueOf("expected a request"))
	}
	data := js.Global().Get("JSON").Call("stringify", args[0]).String()
	var err error
	if data == "" {
		err = errors.New("request is not valid JSON")
	}
	if err != nil {
		return js.Global().Get("Promise").Call("reject", js.ValueOf(err.Error()))
	}
	var request core.GUIRequest
	if err := json.Unmarshal([]byte(data), &request); err != nil {
		return js.Global().Get("Promise").Call("reject", js.ValueOf(err.Error()))
	}

	executor := js.FuncOf(func(_ js.Value, promiseArgs []js.Value) any {
		resolve, reject := promiseArgs[0], promiseArgs[1]
		go func() {
			result, err := engine.Execute(request, func(progress core.GUIProgress) {
				callback := js.Global().Get("mistfallProgress")
				if callback.Type() == js.TypeFunction {
					callback.Invoke(jsonValue(progress))
				}
			})
			if err != nil {
				reject.Invoke(js.ValueOf(err.Error()))
				return
			}
			resolve.Invoke(jsonValue(result))
		}()
		return nil
	})
	promise := js.Global().Get("Promise").New(executor)
	executor.Release()
	return promise
}

func exportCode(_ js.Value, args []js.Value) any {
	if len(args) != 1 {
		return js.Global().Get("Promise").Call("reject", js.ValueOf("expected a session"))
	}
	data := js.Global().Get("JSON").Call("stringify", args[0]).String()
	var session core.GUISession
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return js.Global().Get("Promise").Call("reject", js.ValueOf(err.Error()))
	}
	if !session.HasResult || !session.Result.Possible || len(session.Result.Sets) == 0 {
		return js.Global().Get("Promise").Call("reject", js.ValueOf("only successful results can be exported"))
	}
	code, err := core.ExportCode(session.Request.CharacterClass, session.Result.Sets[0])
	if err != nil {
		return js.Global().Get("Promise").Call("reject", js.ValueOf(err.Error()))
	}
	return js.ValueOf(code)
}

func importCode(_ js.Value, args []js.Value) any {
	if len(args) != 1 {
		return js.Global().Get("Promise").Call("reject", js.ValueOf("expected a build code"))
	}
	session, err := core.DecodeCode(args[0].String())
	if err != nil {
		return js.Global().Get("Promise").Call("reject", js.ValueOf(err.Error()))
	}
	return jsonValue(session)
}

func main() {
	api := js.Global().Get("Object").New()
	for name, function := range map[string]func(js.Value, []js.Value) any{
		"init":       initCore,
		"getOptions": getOptions,
		"execute":    execute,
		"exportCode": exportCode,
		"importCode": importCode,
	} {
		callback := js.FuncOf(function)
		callbacks = append(callbacks, callback)
		api.Set(name, callback)
	}
	js.Global().Set("mistfallCore", api)
	select {}
}
