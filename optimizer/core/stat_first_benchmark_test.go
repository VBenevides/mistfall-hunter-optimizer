package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type statFirstBenchmarkPhase struct {
	Seconds          float64              `json:"seconds"`
	CandidateCount   int                  `json:"candidateCount"`
	CandidateHash    string               `json:"candidateHash"`
	Tested           int                  `json:"tested,omitempty"`
	Possible         bool                 `json:"possible"`
	OptimizationRank *GUIOptimizationRank `json:"optimizationRank,omitempty"`
}

type statFirstBenchmarkResult struct {
	CreatedAt     time.Time               `json:"createdAt"`
	GoVersion     string                  `json:"goVersion"`
	GOOS          string                  `json:"goos"`
	GOARCH        string                  `json:"goarch"`
	GOMAXPROCS    int                     `json:"gomaxprocs"`
	Request       GUIRequest              `json:"request"`
	Generation    statFirstBenchmarkPhase `json:"generation"`
	FullRun       statFirstBenchmarkPhase `json:"fullRun"`
	CandidateSets [][]GUIAffix            `json:"candidateSets"`
}

func statFirstCandidateHash(sets [][]GUIAffix) (string, error) {
	data, err := json.Marshal(sets)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func BenchmarkStatFirstReference5000(b *testing.B) {
	if b.N != 1 {
		b.Fatal("run with -benchtime=1x")
	}
	engine, err := NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	request := GUIRequest{
		CharacterClass:         "Mercenary",
		WeaponClass:            "Sword and Shield",
		MinRarity:              "Gray",
		MaxRarity:              "Gold",
		StatFirst:              true,
		StatFirstReferenceCost: 5000,
	}

	generatedRequest := request
	generatedRequest.StatFirstGenerateOnly = true
	started := time.Now()
	generated, err := engine.Execute(generatedRequest)
	if err != nil {
		b.Fatal(err)
	}
	generationSeconds := time.Since(started).Seconds()
	generationHash, err := statFirstCandidateHash(generated.StatFirstCandidateSets)
	if err != nil {
		b.Fatal(err)
	}

	started = time.Now()
	result, err := engine.Execute(request)
	if err != nil {
		b.Fatal(err)
	}
	fullRunSeconds := time.Since(started).Seconds()
	fullRunHash, err := statFirstCandidateHash(result.StatFirstCandidateSets)
	if err != nil {
		b.Fatal(err)
	}
	if generationHash != fullRunHash {
		b.Fatalf("candidate generation changed between phases: %s != %s", generationHash, fullRunHash)
	}

	record := statFirstBenchmarkResult{
		CreatedAt:  time.Now().UTC(),
		GoVersion:  runtime.Version(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		Request:    request,
		Generation: statFirstBenchmarkPhase{
			Seconds:        generationSeconds,
			CandidateCount: len(generated.StatFirstCandidateSets),
			CandidateHash:  generationHash,
			Possible:       generated.Possible,
		},
		FullRun: statFirstBenchmarkPhase{
			Seconds:          fullRunSeconds,
			CandidateCount:   len(result.StatFirstCandidateSets),
			CandidateHash:    fullRunHash,
			Tested:           result.Tested,
			Possible:         result.Possible,
			OptimizationRank: result.OptimizationRank,
		},
		CandidateSets: generated.StatFirstCandidateSets,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		b.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	directory := filepath.Join(filepath.Dir(source), "..", "benchmark")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(directory, fmt.Sprintf("stat-first-reference-5000-%s.json", record.CreatedAt.Format("20060102T150405.000000000Z")))
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		b.Fatal(err)
	}

	b.ReportMetric(generationSeconds, "generation_s/op")
	b.ReportMetric(fullRunSeconds, "full_run_s/op")
	b.ReportMetric(float64(result.Tested), "dp_checks/op")
	b.Logf("wrote %s", path)
}

func BenchmarkStatFirstValidationReference5000(b *testing.B) {
	if b.N != 1 {
		b.Fatal("run with -benchtime=1x")
	}
	engine, err := NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	request := GUIRequest{
		CharacterClass:         "Mercenary",
		WeaponClass:            "Sword and Shield",
		MinRarity:              "Gray",
		MaxRarity:              "Gold",
		StatFirst:              true,
		StatFirstReferenceCost: 5000,
	}
	generationRequest := request
	generationRequest.StatFirstGenerateOnly = true
	generated, err := engine.Execute(generationRequest)
	if err != nil {
		b.Fatal(err)
	}
	request.StatFirstCandidates = generated.StatFirstCandidateSets
	started := time.Now()
	result, err := engine.Execute(request)
	if err != nil {
		b.Fatal(err)
	}
	validationSeconds := time.Since(started).Seconds()
	b.ReportMetric(validationSeconds, "validation_s/op")
	b.ReportMetric(float64(result.Tested), "dp_checks/op")
}
