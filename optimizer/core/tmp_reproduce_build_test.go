package core

import "testing"

func TestTmpReproduceBuild(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	session, err := DecodeCode("4e5UcSrIR1yj9KQLtmjycRThiz86APPl1LeIIP3Ds")
	if err != nil {
		t.Fatal(err)
	}
	for _, strict := range []bool{false, true} {
		request := session.Request
		request.MinRarity, request.MaxRarity = "Gray", "Gold"
		request.MatchTargetStrictly = strict
		request.Affixes = make([]GUIAffix, 0, len(session.Result.Sets[0].Affixes))
		for _, affix := range session.Result.Sets[0].Affixes {
			request.Affixes = append(request.Affixes, GUIAffix{Name: affix.Name, Level: affix.Result, Enabled: true})
		}
		result, err := engine.Execute(request)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("strict=%t possible=%t price=%v sets=%d code=%s", strict, result.Possible, result.OptimizationRank, len(result.Sets), result.Sets[0].Code)
		if len(result.Sets) > 0 {
			for _, piece := range result.Sets[0].Pieces {
				t.Logf("  %s native=%d gems=%+v", piece.Type, piece.NativeID, piece.Gems)
			}
		}
	}
}
