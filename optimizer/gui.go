package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"mistfall/v2/core"
)

//go:embed frontend/*
var frontend embed.FS

type GUIAffix = core.GUIAffix
type GUIRequest = core.GUIRequest
type GUIOptions = core.GUIOptions
type GUIAffixDetails = core.GUIAffixDetails
type GUIPiece = core.GUIPiece
type GUIGem = core.GUIGem
type GUIResultAffix = core.GUIResultAffix
type GUISet = core.GUISet
type GUIResult = core.GUIResult
type GUIProgress = core.GUIProgress
type GUISession = core.GUISession
type GUISavedResult = core.GUISavedResult

type GUIService struct {
	engine   *core.Engine
	progress func(GUIProgress)
}

func newGUIService() (*GUIService, error) {
	engine, err := core.NewEngine()
	if err != nil {
		return nil, err
	}
	return &GUIService{engine: engine}, nil
}

func (service *GUIService) GetOptions() GUIOptions {
	return service.engine.Options()
}

func (service *GUIService) Execute(request GUIRequest) (GUIResult, error) {
	return service.engine.Execute(request, func(progress GUIProgress) {
		if service.progress != nil {
			service.progress(progress)
		}
	})
}

func (service *GUIService) ExportCode(session GUISession) (string, error) {
	if !session.HasResult || !session.Result.Possible || len(session.Result.Sets) == 0 {
		return "", errors.New("only successful results can be exported")
	}
	return core.ExportCode(session.Request.CharacterClass, session.Result.Sets[0])
}

func (service *GUIService) ImportCode(code string) (GUISession, error) {
	return core.DecodeCode(code)
}

func sessionPath() string {
	return filepath.Join(os.TempDir(), "mistfall-hunters-affix-session.json")
}

func (service *GUIService) LoadSession() (GUISession, error) {
	data, err := os.ReadFile(sessionPath())
	if errors.Is(err, os.ErrNotExist) {
		return GUISession{Help: true}, nil
	}
	if err != nil {
		return GUISession{}, err
	}
	var session GUISession
	if err := json.Unmarshal(data, &session); err != nil {
		return GUISession{}, err
	}
	return session, nil
}

func (service *GUIService) SaveSession(session GUISession) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionPath(), data, 0o600)
}

func resultPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", errors.New("result name must be a file name")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executable), "results", name+".json"), nil
}

func (service *GUIService) ListResults() ([]GUISavedResult, error) {
	path, err := resultPath("result")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return []GUISavedResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	results := []GUISavedResult{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			results = append(results, GUISavedResult{Name: strings.TrimSuffix(entry.Name(), ".json"), CreatedAt: info.ModTime().Format(time.RFC3339)})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt > results[j].CreatedAt })
	return results, nil
}

func (service *GUIService) SaveResult(name string, session GUISession) error {
	if !session.HasResult || !session.Result.Possible {
		return errors.New("only successful results can be saved")
	}
	path, err := resultPath(name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (service *GUIService) LoadResult(name string) (GUISession, error) {
	path, err := resultPath(name)
	if err != nil {
		return GUISession{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return GUISession{}, err
	}
	var session GUISession
	if err := json.Unmarshal(data, &session); err == nil {
		return session, nil
	}
	var fallback struct {
		Request GUIRequest `json:"request"`
	}
	if err := json.Unmarshal(data, &fallback); err != nil || len(fallback.Request.Affixes) == 0 {
		return GUISession{}, fmt.Errorf("saved result %q is unreadable", strings.TrimSpace(name))
	}
	return GUISession{Request: fallback.Request}, nil
}

func (service *GUIService) DeleteResult(name string) error {
	path, err := resultPath(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func runGUI() {
	service, err := newGUIService()
	if err != nil {
		log.Fatal(err)
	}
	assets, err := fs.Sub(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name:        "Mistfall Hunter - Equipment Optimizer",
		Description: "Find an affordable equipment and gem set.",
		Services:    []application.Service{application.NewService(service)},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	service.progress = func(progress GUIProgress) {
		app.Event.Emit("optimization-progress", progress)
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Mistfall Hunter - Equipment Optimizer",
		Width:     1100,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		URL:       "/",
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
