package core

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	database, err := os.ReadFile("../../database/db_mistfalldb.sqlite")
	if err != nil {
		panic(err)
	}
	affixes, err := os.ReadFile("../affixes.json")
	if err != nil {
		panic(err)
	}
	ConfigureAssets(database, affixes)
	os.Exit(m.Run())
}

func TestLegacySessionMigration(t *testing.T) {
	var session GUISession
	err := json.Unmarshal([]byte(`{"request":{"characterClass":"Mercenary"},"result":{"possible":true,"sets":[{"targetAffixes":"Aegis 3/7","selectedAffixes":"Aegis 4/7, Stoic 1/7","unusedGemSlots":2,"unusedAffixSlots":3,"pieces":[{"type":"Weapon","rarity":"Purple","name":"Sword","nativeAffixes":"Aegis","gems":"Red (Fortitude - Aegis/Valor), Blue (empty)"}]},{"affixes":"Valor 2/7","pieces":[]}]},"hasResult":true}`), &session)
	if err != nil || session.Request.MatchTargetStrictly || !session.Result.Sets[0].Legacy || len(session.Result.Sets[0].Affixes) != 2 || session.Result.Sets[0].Affixes[0] != (GUIResultAffix{Name: "Aegis", Result: 4, Target: 3}) || len(session.Result.Sets[0].Pieces[0].Gems) != 2 || session.Result.Sets[1].Affixes[0] != (GUIResultAffix{Name: "Valor", Result: 2, Target: 2}) {
		t.Fatalf("legacy session = %#v, %v", session, err)
	}
}

func TestNativeEquipmentMatch(t *testing.T) {
	equipment, _, err := loadDatabase("Mercenary")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range equipment {
		if item.ID == "3050101" {
			id, err := databaseEquipmentID(10, item)
			if err != nil || id != 3050101 {
				t.Fatalf("native equipment = %d, %v", id, err)
			}
			return
		}
	}
	t.Fatal("test item not found")
}

func TestClassStatsKeepNullValues(t *testing.T) {
	stats, err := loadClassStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats["Mercenary"].Health == nil || *stats["Mercenary"].Health != 618 {
		t.Fatalf("mercenary stats = %#v", stats["Mercenary"])
	}
	if stats["Seer"].Stamina != nil {
		t.Fatalf("seer stamina = %#v, want nil", stats["Seer"].Stamina)
	}
}

func TestBlockedAffixes(t *testing.T) {
	blocked := blockedAffixNames([]GUIAffix{{Name: "Aegis", Blocked: true}})
	if !hasBlockedAffix([]Affix{{Name: "aegis", Level: 1}}, blocked) {
		t.Fatal("blocked affix was not detected")
	}
	if hasBlockedAffix([]Affix{{Name: "Valor", Level: 1}}, blocked) {
		t.Fatal("unblocked affix was detected")
	}
}

func TestNativeCodeRoundTrip(t *testing.T) {
	for _, test := range []struct {
		code, firstAffix string
	}{
		{"Gtf38QvBCKZbq1SSbQQ09WWl1Teu1HBEO5faUa", "Aegis"},
		{"Gtf32jMuNFLcvMCH1lF89qoGfavG6YC0Jk24dE", "Stoic"},
	} {
		session, err := DecodeCode(test.code)
		hasAffix := slices.ContainsFunc(session.Result.Sets[0].Affixes, func(affix GUIResultAffix) bool {
			return affix.Name == test.firstAffix && affix.Result > 0
		})
		hasAttributes := slices.ContainsFunc(session.Result.Sets[0].Pieces, func(piece GUIPiece) bool {
			return len(piece.Attributes) > 0
		})
		if err != nil || session.Request.CharacterClass != "Mercenary" || session.Result.Sets[0].Pieces[0].NativeAffixes != test.firstAffix || !hasAffix || !hasAttributes {
			t.Fatalf("decoded session = %#v, %v", session, err)
		}
		encoded, err := ExportCode(session.Request.CharacterClass, session.Result.Sets[0])
		if err != nil {
			t.Fatalf("round-trip code = %q, %v", encoded, err)
		}
		if _, err := DecodeCode(encoded); err != nil {
			t.Fatalf("generated code = %q, %v", encoded, err)
		}
	}
}

func TestNativeCodeAddsWhiteSecondaryWeapon(t *testing.T) {
	session, err := DecodeCode("Gtf38QvBCKZbq1SSbQQ09WWl1Teu1HBEO5faUa")
	if err != nil {
		t.Fatal(err)
	}
	set := session.Result.Sets[0]
	primary := set.Pieces[0]
	set.Pieces = slices.DeleteFunc(set.Pieces, func(piece GUIPiece) bool { return piece.Type == "Secondary" })
	classID, err := nativeClassID(session.Request.CharacterClass)
	if err != nil {
		t.Fatal(err)
	}
	secondary, ok := secondaryWeapon(classID, primary, secondaryWeaponWhite, nil, nil)
	if !ok || secondary.Rarity != "White" || secondary.NativeID%10000 == primary.NativeID%10000 {
		t.Fatalf("secondary weapon = %#v, primary = %#v", secondary, primary)
	}
	withoutSecondary, err := ExportCode(session.Request.CharacterClass, set)
	if err != nil {
		t.Fatal(err)
	}
	set.Pieces = append(set.Pieces, secondary)
	code, err := ExportCode(session.Request.CharacterClass, set)
	if err != nil || code == withoutSecondary {
		t.Fatalf("secondary weapon changed code: %q, %v", code, err)
	}
	imported, err := DecodeCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Result.Sets[0].Pieces) != len(set.Pieces) {
		t.Fatalf("imported pieces = %#v", imported.Result.Sets[0].Pieces)
	}
}

func TestNativeCodeUsesAlternateWeaponTypeForWhiteSecondary(t *testing.T) {
	session, err := DecodeCode("Gtf33cbKdLvNltRzIebzDmAl3J2xgKR75cyPpY")
	if err != nil {
		t.Fatal(err)
	}
	classID, err := nativeClassID(session.Request.CharacterClass)
	if err != nil {
		t.Fatal(err)
	}
	var primary GUIPiece
	for _, piece := range session.Result.Sets[0].Pieces {
		if piece.Type == "Weapon" {
			primary = piece
			break
		}
	}
	secondary, ok := secondaryWeapon(classID, primary, secondaryWeaponWhite, nil, nil)
	if !ok || secondary.NativeID != 3021001 {
		t.Fatalf("secondary weapon = %#v, primary = %#v", secondary, primary)
	}
}

func TestMatchedSecondaryKeepsCompatibleGems(t *testing.T) {
	session, err := DecodeCode("17lpUcxR1roFJzaMOlCWTFsQuPCKRXSbmg6kV9Tv")
	if err != nil {
		t.Fatal(err)
	}
	var primary GUIPiece
	for _, piece := range session.Result.Sets[0].Pieces {
		if piece.Type == "Weapon" {
			primary = piece
		}
	}
	secondary, ok := secondaryWeapon(15, primary, secondaryWeaponMatched, nil, nil)
	if !ok || secondary.NativeID != 3040901 || len(secondary.Gems) != len(primary.Gems) {
		t.Fatalf("secondary=%+v primary=%+v", secondary, primary)
	}
	code, err := ExportCode(session.Request.CharacterClass, GUISet{Pieces: []GUIPiece{primary, secondary}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCode(code); err != nil {
		t.Fatalf("generated code=%s: %v", code, err)
	}
}

func TestImportedBuildOptimizesAtOriginalRarity(t *testing.T) {
	session, err := DecodeCode("17lpUcxR1roFJzaMOlCWTFsQuPCKRXSbmg6kV9Tv")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"Smiting": 3, "Burst": 3, "Vitality": 2, "Bulwark": 2, "Elusive": 3, "Valor": 3}
	if len(session.Request.Affixes) != len(want) {
		t.Fatalf("imported targets = %#v", session.Request.Affixes)
	}
	for _, affix := range session.Request.Affixes {
		if want[affix.Name] != affix.Level {
			t.Fatalf("imported target = %#v", affix)
		}
	}
	session.Request.MinRarity, session.Request.MaxRarity = "Gray", "Gold"
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Execute(session.Request)
	if err != nil || !result.Possible || result.OptimizationRank == nil || result.OptimizationRank.RaritySum != 32 {
		t.Fatalf("optimized imported build = %#v, %v", result, err)
	}
	for _, piece := range result.Sets[0].Pieces {
		if piece.Rarity != "Blue" {
			t.Fatalf("optimized piece rarity = %#v", piece)
		}
	}
	code, err := ExportCode(session.Request.CharacterClass, result.Sets[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCode(code); err != nil {
		t.Fatalf("optimized build code = %s: %v", code, err)
	}
}

func TestMatchedSecondaryFindsEquivalentGemVariant(t *testing.T) {
	_, gems, err := loadDatabase("Withered Knight")
	if err != nil {
		t.Fatal(err)
	}
	primary := GUIPiece{Type: "Weapon", NativeID: 3030909, Gems: []GUIGem{{NativeID: 222102}}}
	secondary, ok := secondaryWeapon(15, primary, secondaryWeaponMatched, gems, nil)
	if !ok || secondary.NativeID != 3031009 || len(secondary.Gems) != 1 || secondary.Gems[0].NativeID != 221107 {
		t.Fatalf("secondary=%+v", secondary)
	}
	code, err := ExportCode("Withered Knight", GUISet{Pieces: []GUIPiece{primary, secondary}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCode(code); err != nil {
		t.Fatalf("generated code=%s: %v", code, err)
	}
}

func TestReportedSecondaryWeaponCode(t *testing.T) {
	const input = "lfMCD6Uit7xh1XnYKbV60wLJTTE"
	session, err := DecodeCode(input)
	if err != nil {
		t.Fatal(err)
	}
	classID, err := nativeClassID(session.Request.CharacterClass)
	if err != nil {
		t.Fatal(err)
	}
	secondaryID, err := exportedSecondaryID(session.Result.Sets[0].Code, classID)
	if err != nil {
		t.Fatal(err)
	}
	if secondaryID != 3010901 || session.Result.Sets[0].Code != input {
		t.Fatalf("secondary weapon ID = %d, code = %q", secondaryID, session.Result.Sets[0].Code)
	}
}

func TestOptimizationKeepsLowestRarityWhenGoldIsAllowed(t *testing.T) {
	session, err := DecodeCode("Gtf33cbKdLvNltRzIebzDmAl3J2xgKR75cyPpY")
	if err != nil {
		t.Fatal(err)
	}
	session.Request.MinRarity = "Gray"
	session.Request.MaxRarity = "Gold"
	session.Request.MatchTargetStrictly = true
	session.Request.Affixes = []GUIAffix{
		{Name: "Aegis", Level: 3, Enabled: true},
		{Name: "Smiting", Level: 2, Enabled: true},
		{Name: "Elusive", Level: 3, Enabled: true},
		{Name: "Valor", Level: 4, Enabled: true},
		{Name: "Vitality", Level: 4, Enabled: true},
	}
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Execute(session.Request)
	if err != nil || !result.Possible || result.OptimizationRank == nil || result.OptimizationRank.RaritySum != 32 {
		t.Fatalf("lowest-rarity result = %#v, %v", result, err)
	}
	for _, piece := range result.Sets[0].Pieces {
		if piece.Rarity != "Blue" {
			t.Fatalf("piece rarity = %#v, want all Blue", result.Sets[0].Pieces)
		}
	}
}

func TestNativeCodeAddsWhiteAlternateWeaponForEveryClassAndPrimary(t *testing.T) {
	for classID, className := range nativeClassNames {
		classID, className := classID, className
		t.Run(className, func(t *testing.T) {
			primaryIDs := nativeTables.EquipmentByClassSlot[strconv.Itoa(classID)]["10"]
			for _, primaryID := range primaryIDs {
				if primaryID == 0 {
					continue
				}
				primary, ok := nativeEquipment(classID, primaryID)
				if !ok || primary.Slot != "primary" {
					continue
				}
				primaryPiece := GUIPiece{Type: "Weapon", NativeID: primaryID}
				secondary, hasSecondary := secondaryWeapon(classID, primaryPiece, secondaryWeaponWhite, nil, nil)
				pieces := []GUIPiece{primaryPiece}
				if hasSecondary {
					pieces = append(pieces, secondary)
				}
				code, err := ExportCode(className, GUISet{Pieces: pieces})
				if err != nil {
					t.Fatalf("primary %d: export failed: %v", primaryID, err)
				}
				secondaryID, err := exportedSecondaryID(code, classID)
				if err != nil {
					t.Fatalf("primary %d: %v", primaryID, err)
				}
				if !hasSecondary {
					if secondaryID != 0 {
						t.Fatalf("primary=%+v has unexpected secondary %d", primary, secondaryID)
					}
					continue
				}
				if secondaryID != secondary.NativeID {
					t.Fatalf("primary=%+v secondary=%d, want %d", primary, secondaryID, secondary.NativeID)
				}
				secondaryConfig, ok := nativeEquipment(classID, secondaryID)
				if !ok || secondaryConfig.Rarity != "Common" || nativeWeaponType(secondaryConfig) == nativeWeaponType(primary) {
					t.Fatalf("primary=%+v secondary=%+v", primary, secondaryConfig)
				}
			}
		})
	}
}

func exportedSecondaryID(code string, classID int) (int, error) {
	payload, err := base62Decode(code)
	if err != nil {
		return 0, err
	}
	position := 24 + 10 + 4
	configs := nativeTables.EquipmentByClassSlot[strconv.Itoa(classID)]
	for _, slot := range nativeCodeSlots {
		index, next, err := readCodeBits(payload, position, 10)
		if err != nil {
			return 0, err
		}
		position = next
		choices := configs[strconv.Itoa(slot)]
		if index >= len(choices) {
			return 0, fmt.Errorf("invalid equipment index %d for slot %d", index, slot)
		}
		id := choices[index]
		if slot == nativeSlotIDs["secondary"] {
			return id, nil
		}
		if id != 0 {
			config, ok := nativeEquipment(classID, id)
			if !ok {
				return 0, fmt.Errorf("unknown native equipment config: %d", id)
			}
			position += 10 * len(config.Holes)
		}
	}
	return 0, fmt.Errorf("secondary weapon slot is missing")
}

func TestNativeCodeNormalizesSpearAndShield(t *testing.T) {
	code, err := ExportCode("Withered Knight", GUISet{Pieces: []GUIPiece{{Type: "Weapon", NativeID: 3011001}}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := DecodeCode(code)
	if err != nil || session.Request.WeaponClass != "Spear and Shield" {
		t.Fatalf("imported weapon = %q, %v", session.Request.WeaponClass, err)
	}
}

func TestExplicitNonStatTargetRemainsAvailable(t *testing.T) {
	session, err := DecodeCode("17lpVCH3af6gzToaI49auCk6qJ29coYRehd1Ng0W")
	if err != nil {
		t.Fatal(err)
	}
	session.Request.MinRarity = "White"
	session.Request.MaxRarity = "Blue"
	session.Request.MatchTargetStrictly = true
	session.Request.Affixes = []GUIAffix{{Name: "Focused", Level: 4, Enabled: true}}
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Execute(session.Request)
	if err != nil || !result.Possible || result.Closest {
		t.Fatalf("focused target result = %#v, %v", result, err)
	}
	for _, affix := range result.Sets[0].Affixes {
		if affix.Name == "Focused" && affix.Result != 4 {
			t.Fatalf("strict focused result = %#v", result.Sets[0].Affixes)
		}
	}
}

func TestOptimizationProgressIsThrottled(t *testing.T) {
	reports := 0
	progress := optimizationProgress{
		updated: time.Now().Add(-time.Second),
		report:  func(string, string, int, int, int) { reports++ },
	}
	for range 99 {
		progress.test("same", "weapon", 1, 8)
	}
	if reports != 0 {
		t.Fatalf("reported before 100 combinations")
	}
	progress.test("same", "weapon", 1, 8)
	if reports != 1 {
		t.Fatalf("reports after 100 combinations = %d", reports)
	}
	progress.updated = time.Now().Add(time.Hour)
	for range 100 {
		progress.test("same", "weapon", 1, 8)
	}
	if reports != 1 {
		t.Fatalf("reported again before 100ms")
	}
}

func TestGemChoicesKeepColorAndPreferExactTier(t *testing.T) {
	gems := []Item{
		{ID: "amethyst-1", Gem: GemData{AffixGemType: 2, AffixGemLevel: 1, Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
		{ID: "amethyst-2", Gem: GemData{AffixGemType: 2, AffixGemLevel: 2, Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
		{ID: "agate-2", Gem: GemData{AffixGemType: 1, AffixGemLevel: 2}},
	}
	choices := gemChoices(gems, Socket{Type: 2, Level: 2})
	if len(choices) != 2 || compatible(gems[2], Socket{Type: 2, Level: 2}) {
		t.Fatalf("tier-2 choices = %#v", choices)
	}
	options := itemOptions(Item{ItemSockets: []Socket{{Type: 2, Level: 2}}}, gems, map[string]int{"aegis": 0}, []int{1})
	for _, option := range options {
		if option.Coverage[0] == 1 && option.Selected[0] != "amethyst-2" {
			t.Fatalf("preferred gem = %#v", option)
		}
	}
	choices = gemChoices(gems[:1], Socket{Type: 2, Level: 2})
	if len(choices) != 1 || choices[0].ID != "amethyst-1" || compatible(gems[1], Socket{Type: 2, Level: 1}) {
		t.Fatalf("tier-1 fallback = %#v", choices)
	}
}

func TestAffixStatClassificationFiltersNonStatGems(t *testing.T) {
	cases := map[string]struct {
		levels map[string]string
		want   []string
	}{
		"Valor":     {map[string]string{"1": "Attack +1%. Defense Penetration +1%."}, []string{"Attack"}},
		"Stoic":     {map[string]string{"1": "Physical Resistance +1%. Restores 10% Health."}, []string{"Defense", "HP"}},
		"Eloquence": {map[string]string{"1": "Chanting Speed +3%."}, []string{"None"}},
	}
	for name, test := range cases {
		if got := inferredAffixStats(GUIAffixDetails{Levels: test.levels}); !slices.Equal(got, test.want) {
			t.Fatalf("%s stats = %v, want %v", name, got, test.want)
		}
	}
	details := map[string]GUIAffixDetails{
		"aegis":     {Stats: []string{"Defense"}},
		"eloquence": {Stats: []string{"None"}},
	}
	gems := []Item{
		{ID: "stat", Gem: GemData{Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
		{ID: "utility", Gem: GemData{Affixes: []Affix{{Name: "Eloquence", Level: 1}}}},
	}
	if got := filterGemsByStats(gems, nil, details); len(got) != 1 || got[0].ID != "stat" {
		t.Fatalf("non-stat gems = %#v", got)
	}
	if got := filterGemsByStats(gems, map[string]bool{"eloquence": true}, details); len(got) != 2 {
		t.Fatalf("explicit non-stat target = %#v", got)
	}
}

func TestRhombGemsAreAmethystTierTwo(t *testing.T) {
	for _, name := range []string{"Zeal-Vitality Purple Rhomb", "Zeal-Vitality Purple Rhombus"} {
		gem := Item{Name: name, Gem: GemData{AffixGemType: 5, AffixGemLevel: 2}}
		if gemType(gem) != 2 || gemActualColors[gemType(gem)] != "Pink" || !compatible(gem, Socket{Type: 2, Level: 2}) || compatible(gem, Socket{Type: 2, Level: 1}) {
			t.Fatalf("rhomb gem compatibility for %q is incorrect", name)
		}
	}
	normalAny := Item{Name: "Any Gem", Gem: GemData{AffixGemType: 5, AffixGemLevel: 1}}
	if gemType(normalAny) != 5 || !compatible(normalAny, Socket{Type: 2, Level: 1}) || !compatible(normalAny, Socket{Type: 5, Level: 1}) {
		t.Fatal("normal type-5 gem was not treated as universal")
	}
}

func TestOnyxGemsAreAgate(t *testing.T) {
	gem := Item{Name: "Resolve Onyx", Gem: GemData{AffixGemType: 5, AffixGemLevel: 1}}
	if gemType(gem) != 1 || !compatible(gem, Socket{Type: 1, Level: 1}) || !compatible(gem, Socket{Type: 5, Level: 1}) || compatible(gem, Socket{Type: 2, Level: 1}) || compatible(gem, Socket{Type: 3, Level: 1}) || compatible(gem, Socket{Type: 4, Level: 1}) {
		t.Fatal("onyx gem was not restricted to agate and universal sockets")
	}
}

func TestParallelSolveKeepsCheapestCombination(t *testing.T) {
	items := []Item{}
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		for _, price := range []float64{10, 1} {
			item := Item{Name: slot, Grade: 1, MainCategory: category, SubName: slot, MinPrice: price, MaxPrice: price, RecommendedPrice: price}
			if slot == "weapon" {
				item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
			}
			items = append(items, item)
		}
	}
	levels := map[string]int{}
	for _, slot := range slotOrder {
		levels[slot] = 1
	}
	positions, limits := map[string]int{"aegis": 0}, []int{1}
	bounds := buildUpperBounds(items, nil, positions, limits)
	for _, parallel := range []bool{false, true} {
		result := solve(items, nil, levels, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, positions, limits, bounds, "same", nil, parallel)
		if result == nil || !result.Possible {
			t.Fatalf("parallel=%t result = %#v", parallel, result)
		}
	}
	result, err := optimize(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 0, 1, 1, nil, false)
	if err != nil || !result.Possible || result.WeaponLevel != 1 {
		t.Fatalf("any-rarity result = %#v, %v", result, err)
	}
	items = append(items, Item{Name: "weapon-2", Grade: 2, MainCategory: "weapon", SubName: "weapon", Equipment: EquipmentData{Affixes: []Affix{{Name: "Aegis", Level: 1}}}})
	result, err = optimize(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 2, 1, 2, nil, false)
	if err != nil || !result.Possible || result.ArmorLevel != 1 || result.WeaponLevel != 2 {
		t.Fatalf("fixed weapon rarity result = %#v, %v", result, err)
	}
}

func TestOptimizeAllowsNoTargets(t *testing.T) {
	items := make([]Item, 0, len(slotOrder))
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		items = append(items, Item{ID: slot, Name: slot, MainCategory: category, SubName: slot, Grade: 1})
	}
	result, err := optimizeConfigured(items, nil, nil, 1, 1, nil, false, nil, rarityUpgradeOrder, 0)
	if err != nil || result == nil || !result.Possible || len(result.Pieces) != len(slotOrder) {
		t.Fatalf("no-target result = %#v, %v", result, err)
	}
}

func TestMinimumAffixLevelRequirement(t *testing.T) {
	items := make([]Item, 0, len(slotOrder))
	for index, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		affixes := []Affix{}
		if index < 6 {
			affixes = []Affix{{Name: "Aegis", Level: 2}}
		}
		items = append(items, Item{ID: slot, Name: slot, MainCategory: category, SubName: slot, Grade: 1, Equipment: EquipmentData{Affixes: affixes}})
	}
	requirements := appendMinimumAffixRequirement(nil, 12)
	positions, limits := map[string]int{totalAffixLevelKey: 0}, []int{12}
	result := solve(items, nil, nil, requirements, positions, limits, buildUpperBounds(items, nil, positions, limits), "test", nil, false)
	if result == nil || !result.Possible {
		t.Fatalf("minimum-affix result = %#v", result)
	}
	total := 0
	for _, piece := range result.Pieces {
		if affixes, ok := piece.NativeAffixes.([]Affix); ok {
			for _, affix := range affixes {
				total += affix.Level
			}
		}
	}
	if total < 12 {
		t.Fatalf("minimum-affix total = %d", total)
	}
}

func TestExecuteAllowsNoTargets(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass:  "Mercenary",
		WeaponClass:     "Sword and Shield",
		SecondaryWeapon: secondaryWeaponWhite,
		MinRarity:       "White",
		MaxRarity:       "White",
	})
	if err != nil || !result.Possible || len(result.Sets) != 1 {
		t.Fatalf("no-target execute result = %#v, %v", result, err)
	}
	if len(result.Sets[0].Pieces) != len(slotOrder)+1 || result.Sets[0].Pieces[1].Type != "Secondary" || result.Sets[0].Pieces[1].Rarity != "White" {
		t.Fatalf("white secondary = %#v", result.Sets[0].Pieces)
	}
}

func TestExecuteMatchesPrimarySecondaryWeapon(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass:  "Withered Knight",
		WeaponClass:     "Greatsword",
		SecondaryWeapon: secondaryWeaponMatched,
		MinRarity:       "Green",
		MaxRarity:       "Green",
	})
	if err != nil || !result.Possible || len(result.Sets) != 1 {
		t.Fatalf("matched secondary result = %#v, %v", result, err)
	}
	if result.Sets[0].Code == "" {
		t.Fatal("matched secondary did not produce a build code")
	}
	primary, secondary := result.Sets[0].Pieces[0], result.Sets[0].Pieces[1]
	primaryConfig, primaryOK := nativeEquipment(15, primary.NativeID)
	secondaryConfig, secondaryOK := nativeEquipment(15, secondary.NativeID)
	if !primaryOK || !secondaryOK || secondary.Type != "Secondary" || secondary.Rarity != primary.Rarity || secondary.NativeAffixes != primary.NativeAffixes || nativeWeaponType(primaryConfig) == nativeWeaponType(secondaryConfig) || !strings.Contains(secondary.Name, "Polearm and Shield") {
		t.Fatalf("matched secondary = %#v, primary = %#v", secondary, primary)
	}
}

func TestExecutedBuildMatchesDecodedSecondaryAttributes(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass:  "Mercenary",
		WeaponClass:     "Sword and Shield",
		SecondaryWeapon: secondaryWeaponWhite,
		MinRarity:       "Green",
		MaxRarity:       "Green",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCode(result.Sets[0].Code)
	if err != nil {
		t.Fatal(err)
	}
	current, loaded := result.Sets[0], decoded.Result.Sets[0]
	var currentSecondary, loadedSecondary GUIPiece
	for _, piece := range current.Pieces {
		if piece.Type == "Secondary" {
			currentSecondary = piece
		}
	}
	for _, piece := range loaded.Pieces {
		if piece.Type == "Secondary" {
			loadedSecondary = piece
		}
	}
	if current.Price != loaded.Price || currentSecondary.Attributes["attack"] != loadedSecondary.Attributes["attack"] {
		t.Fatalf("executed build differs from decoded secondary: current=%+v loaded=%+v", current, loaded)
	}
}

func TestSecondaryWeaponIsForcedNoneForSingleWeaponClass(t *testing.T) {
	engine := &Engine{options: GUIOptions{WeaponClasses: map[string][]string{"Solo": {"Sword and Shield"}}}}
	request, err := engine.normalizeSecondaryWeapon(GUIRequest{CharacterClass: "Solo", SecondaryWeapon: secondaryWeaponMatched})
	if err != nil || request.SecondaryWeapon != secondaryWeaponNone {
		t.Fatalf("single-weapon secondary = %#v, %v", request, err)
	}
}

func TestExecuteHonorsMinimumAffixLevel(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass:    "Mercenary",
		WeaponClass:       "Sword and Shield",
		MinRarity:         "White",
		MaxRarity:         "Gold",
		MinimumAffixLevel: 12,
	})
	if err != nil || !result.Possible || len(result.Sets) != 1 {
		t.Fatalf("minimum-affix execute result = %#v, %v", result, err)
	}
	total := 0
	for _, affix := range result.Sets[0].Affixes {
		total += affix.Result
	}
	if total < 12 {
		_, gems, loadErr := loadDatabase("Mercenary")
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		byNativeID := map[int]Item{}
		for _, gem := range gems {
			if nativeID, idErr := databaseGemID(gem); idErr == nil {
				byNativeID[nativeID] = gem
			}
		}
		total = 0
		for _, piece := range result.Sets[0].Pieces {
			for _, gem := range piece.Gems {
				for _, affix := range byNativeID[gem.NativeID].Gem.Affixes {
					total += affix.Level
				}
			}
		}
	}
	if total < 12 {
		t.Fatalf("minimum-affix execute total = %d", total)
	}
}

func TestExecuteValidatesMinimumAffixLevel(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(GUIRequest{
		CharacterClass:    "Mercenary",
		WeaponClass:       "Sword and Shield",
		MinRarity:         "White",
		MaxRarity:         "White",
		MinimumAffixLevel: 1,
	})
	if err == nil {
		t.Fatal("invalid minimum-affix level was accepted")
	}
}

func TestStatFirstFrontierStopsAfterLowGain(t *testing.T) {
	results := []statFirstResult{
		{price: 10, score: 0},
		{price: 20, score: 0.5},
		{price: 30, score: 0.501},
		{price: 40, score: 0.502},
		{price: 50, score: 0.503},
	}
	frontier := statFirstFrontier(results)
	if len(frontier) != len(results) {
		t.Fatalf("stat-first frontier = %#v", frontier)
	}
	selected := selectStatFirstResult(frontier, 0.01, 0)
	if selected.price != 20 {
		t.Fatalf("stat-first marginal selection = %#v", selected)
	}
}

func TestStatFirstFrontierUsesAutomaticQuarterGain(t *testing.T) {
	results := []statFirstResult{
		{price: 10, score: 0},
		{price: 20, score: 0.5},
		{price: 30, score: 0.503},
		{price: 40, score: 0.5035},
		{price: 50, score: 0.5036},
	}
	selected := selectStatFirstResult(statFirstFrontier(results), 0, 0)
	if selected.price != 20 {
		t.Fatalf("automatic stat-first marginal selection = %#v", selected)
	}
}

func TestStatFirstScoreCombinesAttackAndDamage(t *testing.T) {
	results := []statFirstResult{
		{result: GUIResult{OptimizationRank: &GUIOptimizationRank{Stats: [4]float64{0, 101.8, 0, 0}}}},
		{result: GUIResult{OptimizationRank: &GUIOptimizationRank{Stats: [4]float64{0, 100, 0, 0}, Damage: 6}}},
	}
	normalizeStatFirstScores(results, [4]int{1, 0, 2, 3})
	if results[1].score <= results[0].score {
		t.Fatalf("attack-priority score = %#v", results)
	}
}

func TestStatFirstScoreIncludesDefensePenetration(t *testing.T) {
	results := []statFirstResult{
		{result: GUIResult{OptimizationRank: &GUIOptimizationRank{Stats: [4]float64{0, 100, 0, 0}, DefensePenetration: 1.8}}},
		{result: GUIResult{OptimizationRank: &GUIOptimizationRank{Stats: [4]float64{0, 102.6, 0, 0}}}},
	}
	normalizeStatFirstScores(results, [4]int{1, 0, 2, 3})
	if results[0].score <= results[1].score {
		t.Fatalf("Defense Penetration attack equivalent = %#v", results)
	}
}

func TestStatFirstCandidateTiePrefersConcentratedLevels(t *testing.T) {
	common := [4]float64{0, 100, 0, 0}
	centralized := statFirstCandidateState{levels: map[string]int{"strife": 3}, stats: common}
	distributed := statFirstCandidateState{levels: map[string]int{"strife": 2, "fervor": 1}, stats: common}
	if !betterStatFirstCandidate(centralized, distributed, [4]int{1, 0, 2, 3}) {
		t.Fatal("equal gains should prefer concentrated affix levels")
	}
}

func TestStatFirstCandidatesKeepThresholdAlternatives(t *testing.T) {
	states := map[int][]statFirstCandidateState{5: {
		{levels: map[string]int{"valor": 5}, thresholds: 1, stats: [4]float64{0, 100, 0, 0}},
		{levels: map[string]int{"valor": 3, "fervor": 2}, thresholds: 2, stats: [4]float64{0, 90, 0, 0}},
	}}
	candidates := chooseStatFirstCandidates(states, GUIRequest{}, nil, 5, 2, nil)
	if len(candidates) != 2 || candidates[1].thresholds != 2 {
		t.Fatalf("threshold alternative was not preserved: %#v", candidates)
	}
}

func TestStatFirstReferenceCostDoesNotPreferPriceProximity(t *testing.T) {
	results := []statFirstResult{
		{price: 10, score: 0},
		{price: 20, score: 0.4},
		{price: 30, score: 0.5},
	}
	selected := selectStatFirstResult(statFirstFrontier(results), 0, 10)
	if selected.price != 20 {
		t.Fatalf("cost-ceiling selection = %#v", selected)
	}
}

func TestReferenceCostFiltersValidatedPrices(t *testing.T) {
	results := []statFirstResult{{price: 1000}, {price: 1400}, {price: 2000}}
	filtered, fallback := filterStatFirstResultsByReferenceCost(results, 1500)
	if fallback || len(filtered) != 2 || filtered[0].price != 1000 || filtered[1].price != 1400 {
		t.Fatalf("reference-cost price filter = %#v, fallback=%v", filtered, fallback)
	}
	filtered, fallback = filterStatFirstResultsByReferenceCost([]statFirstResult{{price: 2000}, {price: 2200}}, 1500)
	if !fallback || len(filtered) != 2 {
		t.Fatalf("reference-cost fallback = %#v, fallback=%v", filtered, fallback)
	}
}

func TestReferenceCandidateTotalsUsesPriceLookupRange(t *testing.T) {
	engine := &Engine{priceLookup: map[int]float64{8: 700, 9: 900, 10: 1000, 11: 1100, 12: 1400}}
	totals := engine.referenceCandidateTotals(1000, 0, 32)
	for _, total := range []int{8, 9, 10, 11} {
		if !totals[total] {
			t.Fatalf("reference-cost total %d was not selected: %#v", total, totals)
		}
	}
	if totals[12] {
		t.Fatalf("reference-cost selected an over-ceiling total: %#v", totals)
	}
	if totals[0] {
		t.Fatalf("reference-cost selected an empty candidate: %#v", totals)
	}
}

func TestStatFirstCandidatesSortByPriority(t *testing.T) {
	candidates := []statFirstCandidate{
		{affixes: []GUIAffix{{Name: "Low", Level: 1}}, stats: [4]float64{10, 20, 0, 0}},
		{affixes: []GUIAffix{{Name: "High", Level: 1}}, stats: [4]float64{10, 30, 0, 0}},
	}
	sortStatFirstCandidates(candidates, [4]int{1, 0, 2, 3})
	if candidates[0].affixes[0].Name != "High" {
		t.Fatalf("stat-first candidate order = %#v", candidates)
	}
}

func TestStatFirstCreationIsOptIn(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	request := GUIRequest{CharacterClass: "Mercenary", WeaponClass: "Sword and Shield"}
	reported := false
	candidates, err := engine.generateStatFirstCandidates(request, [4]int{0, 1, 2, 3}, maxEquipmentAffixLevels(5), 2, true, func(progress GUIProgress) {
		reported = reported || progress.Stage == "Generating candidates"
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reported {
		t.Fatal("candidate generation did not report progress")
	}
	for _, candidate := range candidates {
		for _, affix := range candidate.affixes {
			if normalize(affix.Name) == "creation" {
				t.Fatal("Creation was included without an explicit target")
			}
		}
	}

	request.Affixes = []GUIAffix{{Name: "Creation", Level: 1}}
	candidates, err = engine.generateStatFirstCandidates(request, [4]int{0, 1, 2, 3}, maxEquipmentAffixLevels(5), 2, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		for _, affix := range candidate.affixes {
			if normalize(affix.Name) == "creation" && affix.Level == 1 {
				return
			}
		}
	}
	t.Fatal("Creation was not preserved as an explicit target")
}

func TestStatFirstCandidateGenerationIsDeterministic(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	request := GUIRequest{CharacterClass: "Mercenary", WeaponClass: "Sword and Shield"}
	parallel, err := engine.generateStatFirstCandidates(request, [4]int{0, 1, 2, 3}, maxEquipmentAffixLevels(5), 4, true)
	if err != nil {
		t.Fatal(err)
	}
	sequential, err := engine.generateStatFirstCandidates(request, [4]int{0, 1, 2, 3}, maxEquipmentAffixLevels(5), 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(statFirstCandidateSignatures(parallel), statFirstCandidateSignatures(sequential)) {
		t.Fatalf("parallel candidate generation changed results: parallel=%v sequential=%v", statFirstCandidateSignatures(parallel), statFirstCandidateSignatures(sequential))
	}
}

func statFirstCandidateSignatures(candidates []statFirstCandidate) []string {
	result := make([]string, len(candidates))
	for index, candidate := range candidates {
		parts := make([]string, len(candidate.affixes))
		for affixIndex, affix := range candidate.affixes {
			parts[affixIndex] = fmt.Sprintf("%s=%d", affix.Name, affix.Level)
		}
		result[index] = strings.Join(parts, ",")
	}
	slices.Sort(result)
	return result
}

func TestExecuteStatFirst(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass:      "Mercenary",
		WeaponClass:         "Sword and Shield",
		MinRarity:           "White",
		MaxRarity:           "Gold",
		MinimumAffixLevel:   8,
		StatFirst:           true,
		MatchTargetStrictly: true,
	}, func(GUIProgress) {})
	if err != nil || !result.Possible || len(result.Sets) != 1 {
		t.Fatalf("stat-first result = %#v, %v", result, err)
	}
	if result.Debug == nil || len(result.Debug.Candidates) == 0 {
		t.Fatal("Stat First candidates were not included")
	}
	if !slices.ContainsFunc(result.Rules, func(rule string) bool { return strings.HasPrefix(rule, "Optimization mode: Stat First") }) {
		t.Fatalf("stat-first rules = %#v", result.Rules)
	}
}

func TestMaxEquipmentAffixLevelsForFixedRarities(t *testing.T) {
	fixed := map[string]int{"weapon": 4, "gauntlets": 4, "necklace": 5}
	if got := maxEquipmentAffixLevelsForFixed(6, fixed, false); got != 22 {
		t.Fatalf("fixed rarity capacity = %d, want 22", got)
	}
}

func TestStatFirstSupportsMixedFixedRarities(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	request := GUIRequest{
		CharacterClass:    "Mercenary",
		WeaponClass:       "Sword and Shield",
		MinRarity:         "White",
		MaxRarity:         "Gold",
		MinimumAffixLevel: 1,
		FixedRarities: map[string]string{
			"weapon": "Blue", "gauntlets": "Blue", "necklace": "Purple",
		},
		StatFirst:           true,
		MatchTargetStrictly: true,
	}
	result, err := service.Execute(request, func(GUIProgress) {})
	if err != nil || !result.Possible || result.Closest {
		t.Fatalf("stat-first fixed rarity result = %#v, %v", result, err)
	}
	for _, set := range result.StatFirstCandidateSets {
		total := 0
		for _, affix := range set {
			total += affix.Level
		}
		if total > 22 {
			t.Fatalf("candidate total = %d, want <= 22", total)
		}
	}
}

func TestConfiguredRaritySettings(t *testing.T) {
	fixed, err := rarityConfiguration(GUIRequest{FixedRarities: map[string]string{"helmet": "White"}}, 1, 2)
	if err != nil || fixed["helmet"] != 2 {
		t.Fatalf("rarity configuration = %#v, %v", fixed, err)
	}
	items := make([]Item, 0, len(slotOrder)+2)
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		items = append(items, Item{Name: slot, Grade: 1, MainCategory: category, SubName: slot})
	}
	items = append(items,
		Item{Name: "weapon-white", Grade: 2, MainCategory: "weapon", SubName: "weapon"},
		Item{Name: "helmet-white", Grade: 2, MainCategory: "armor", SubName: "helmet", Equipment: EquipmentData{Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
	)
	result, err := optimizeConfigured(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1, Max: 1}}, 1, 2, nil, false, fixed, rarityUpgradeOrder, 0)
	if err != nil || result == nil || !result.Possible || result.LevelCombination[1] != 2 {
		t.Fatalf("configured rarity result = %#v, %v", result, err)
	}
}

func TestStatPriority(t *testing.T) {
	strongWeapon := solveState{Stats: [4]float64{11, 0, 0, 0}}
	weakWeapon := solveState{Stats: [4]float64{10, 100, 100, 100}}
	if !strongWeapon.betterThan(&weakWeapon, nil) {
		t.Fatal("weapon damage should have priority by default")
	}
	attack := solveState{Stats: [4]float64{0, 10, 0, 0}}
	defense := solveState{Stats: [4]float64{0, 9, 100, 100}}
	if !attack.betterThan(&defense, nil) {
		t.Fatal("attack should have priority over defense and health")
	}
	healthFirst := solveState{Stats: [4]float64{0, 0, 0, 10}, StatOrder: [4]int{3, 0, 1, 2}}
	attackSecond := solveState{Stats: [4]float64{0, 100, 0, 1}, StatOrder: [4]int{3, 0, 1, 2}}
	if !healthFirst.betterThan(&attackSecond, nil) {
		t.Fatal("reordered health priority was ignored")
	}
	if got := priorityStats(Item{Attributes: map[string]interface{}{"attack": 1, "defence": 2, "maxHealth": 3}}); got != [4]float64{0, 1, 2, 3} {
		t.Fatalf("priority stats = %v", got)
	}
}

func TestAttackPriorityUsesRelevantDamage(t *testing.T) {
	attack := solveState{Stats: [4]float64{0, 100, 0, 0}, Damage: 5, StatOrder: [4]int{1, 0, 2, 3}}
	strongerDamage := solveState{Stats: [4]float64{0, 100, 0, 0}, Damage: 6, StatOrder: [4]int{1, 0, 2, 3}}
	if !strongerDamage.betterThan(&attack, nil) {
		t.Fatal("relevant damage should break an Attack tie")
	}
	moreAttack := solveState{Stats: [4]float64{0, 101.8, 0, 0}, StatOrder: [4]int{1, 0, 2, 3}}
	if compareAttackPriorityStats(strongerDamage.Stats, strongerDamage.Damage, moreAttack.Stats, moreAttack.Damage, strongerDamage.StatOrder) <= 0 {
		t.Fatal("relevant damage should equal Attack for Attack priority")
	}
	brotherhood := [4]float64{0, 12.3, 30, 0}
	strife := [4]float64{0, 10.5, 0, 0}
	if compareAttackPriorityStats(strife, 6, brotherhood, 0, [4]int{0, 1, 2, 3}) <= 0 {
		t.Fatal("Strife 5 should beat Brotherhood 5 after combining Attack and damage")
	}
	if got := compareAttackPriorityStatsWithDefensePenetration([4]float64{0, 7.5, 0, 0}, 0, 1.8, [4]float64{0, 11, 0, 0}, 0, 0, [4]int{1, 0, 2, 3}); got <= 0 {
		t.Fatal("Valor 5 should include Defense Penetration as Attack equivalent")
	}
	if got := parseStatEffect("Attack +3%. Physical Damage +5%. Magic Damage +7%."); got.percent[1] != 3 || got.damage != [2]float64{5, 7} {
		t.Fatalf("damage stat effect = %#v", got)
	}
	if got := parseStatEffect("Attack +7.5%. Defense Penetration +1.8%."); got.percent[1] != 7.5 || got.defensePenetration != 1.8 {
		t.Fatalf("defense penetration stat effect = %#v", got)
	}
	if got := parseStatEffect("Physical Damage +6%. Magic Damage +6%. Physical Damage per stack +2%. Magic Damage per stack +2%."); got.damage != [2]float64{8, 8} {
		t.Fatalf("stacked damage stat effect = %#v", got)
	}
	if got := parseStatEffect("stacking up to 5 times. Physical Damage +2%. Magic Damage +2%. Defense Penetration +2.5%."); got.damage != [2]float64{10, 10} || got.defensePenetration != 2.5 {
		t.Fatalf("Fervor stat effect = %#v", got)
	}
	valor := GUIAffixDetails{Levels: map[string]string{
		"1": "Attack +1.5%.", "2": "Attack +3%.", "3": "Attack +4.5%.", "4": "Attack +6%.", "5": "Attack +7.5%. Defense Penetration +1.8.", "6": "Attack +9%. Defense Penetration +1.8.",
	}}
	if got := affixThresholdCount(valor, 6); got != 1 {
		t.Fatalf("threshold count = %d", got)
	}
}

func TestWeaponOnlyAffixFilter(t *testing.T) {
	items := []Item{
		{ID: "weapon", MainCategory: "weapon", SubName: "Greatsword", Equipment: EquipmentData{Affixes: []Affix{{Name: "Burst", Level: 1}}}},
		{ID: "armor", MainCategory: "armor", SubName: "helmet", Equipment: EquipmentData{Affixes: []Affix{{Name: "Burst", Level: 1}}}},
	}
	filtered := filterWeaponOnlyAffixes(items, "Greatsword", nil, false)
	if len(filtered) != 1 || filtered[0].ID != "weapon" {
		t.Fatalf("weapon-only filter = %#v", filtered)
	}
	targeted := filterWeaponOnlyAffixes(items, "Sword and Shield", map[string]bool{"burst": true}, false)
	if len(targeted) != len(items) {
		t.Fatalf("targeted weapon-only filter = %#v", targeted)
	}
	allowedStrife := []string{"Sword and Shield", "Hammer", "Dagger", "Dual Blades", "Mace", "Greatsword", "Polearm and Shield", "Javelin"}
	for _, weapon := range allowedStrife {
		items := []Item{{ID: "strife-weapon", MainCategory: "weapon", SubName: weapon, Equipment: EquipmentData{Affixes: []Affix{{Name: "Strife", Level: 1}}}}}
		if filtered := filterWeaponOnlyAffixes(items, weapon, nil, false); len(filtered) != 1 {
			t.Fatalf("Strife should be allowed on %s: %#v", weapon, filtered)
		}
	}
	if filtered := filterWeaponOnlyAffixes([]Item{{ID: "strife-bow", MainCategory: "weapon", SubName: "Bow", Equipment: EquipmentData{Affixes: []Affix{{Name: "Strife", Level: 1}}}}}, "Bow", nil, false); len(filtered) != 0 {
		t.Fatalf("Strife should be rejected on Bow: %#v", filtered)
	}
}

func TestSearchShardAssignmentIsStable(t *testing.T) {
	for _, id := range []string{"weapon-1", "weapon-2", "weapon-3"} {
		assigned := searchShardForItem(id, 4)
		if assigned < 0 || assigned >= 4 {
			t.Fatalf("search shard for %q = %d", id, assigned)
		}
		if assigned != searchShardForItem(id, 4) || searchShardForItem(id, 1) != 0 {
			t.Fatalf("search shard assignment is unstable for %q", id)
		}
	}
}

func TestDamageTypeUsesClass(t *testing.T) {
	if weaponDamageType("Staff") != 1 || weaponDamageType("Catalyst") != 1 || weaponDamageType("Mace") != 1 || weaponDamageType("Greatsword") != 0 {
		t.Fatal("weapon damage type mapping is incorrect")
	}
}

func TestHealthPriorityUsesBaseStatsAndAffixes(t *testing.T) {
	items := make([]Item, 0, len(slotOrder)+2)
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		item := Item{ID: slot + "-base", Name: slot + " base", MainCategory: category, SubName: slot, Grade: 1}
		if slot == "weapon" {
			item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
		}
		items = append(items, item)
	}
	items = append(items,
		Item{ID: "ring-hp", Name: "ring hp", MainCategory: "armor", SubName: "ring", Grade: 1, Attributes: map[string]interface{}{"maxHealth": 40}},
		Item{ID: "ring-tenacious", Name: "ring tenacious", MainCategory: "armor", SubName: "ring", Grade: 1, Equipment: EquipmentData{Affixes: []Affix{{Name: "Tenacious", Level: 1}}}},
	)
	health := 618.0
	result, err := optimizeConfiguredWithStats(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 1, 1, nil, false, map[string]int{}, rarityUpgradeOrder, 0, ClassStats{Health: &health}, map[string]GUIAffixDetails{
		"Tenacious": {Levels: map[string]string{"1": "Maximum Health +10%."}},
	}, [4]int{3, 0, 1, 2})
	if err != nil || result == nil || !result.Possible || result.Pieces[7].Name != "ring tenacious" {
		t.Fatalf("health priority result = %#v, %v", result, err)
	}
}

func TestHealthPriorityKeepsBonusHealthGem(t *testing.T) {
	items := make([]Item, 0, len(slotOrder))
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		item := Item{ID: slot, Name: slot, MainCategory: category, SubName: slot, Grade: 1}
		if slot == "weapon" {
			item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
		}
		if slot == "ring" {
			item.ItemSockets = []Socket{{Type: 1, Level: 1}}
		}
		items = append(items, item)
	}
	baseHealth := 618.0
	gems := []Item{
		{ID: "cheap", RecommendedPrice: 1, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Elusive", Level: 1}}}},
		{ID: "tenacious", RecommendedPrice: 100, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Tenacious", Level: 1}}}},
	}
	result, err := optimizeConfiguredWithStats(items, gems, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 1, 1, nil, false, map[string]int{}, rarityUpgradeOrder, 0, ClassStats{Health: &baseHealth}, map[string]GUIAffixDetails{
		"Tenacious": {Levels: map[string]string{"1": "Maximum Health +10%."}},
	}, [4]int{3, 0, 1, 2})
	if err != nil || result == nil || result.Pieces[7].GemSlots[0].Gem == nil || result.Pieces[7].GemSlots[0].Gem.ID != "tenacious" {
		t.Fatalf("bonus health gem result = %#v, %v", result, err)
	}
}

func TestNoFillPrefersTargetOnlyEquipment(t *testing.T) {
	items := make([]Item, 0, len(slotOrder)+1)
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		item := Item{ID: slot, Name: slot, MainCategory: category, SubName: slot, Grade: 1}
		if slot == "weapon" {
			item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
		}
		items = append(items, item)
	}
	items = append(items, Item{ID: "a-helmet-bonus", Name: "helmet bonus", MainCategory: "armor", SubName: "helmet", Grade: 1, Equipment: EquipmentData{Affixes: []Affix{{Name: "Wrath", Level: 1}}}})
	result, err := optimizeConfiguredWithStats(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 1, 1, &optimizationProgress{restrictGems: true}, false, map[string]int{}, rarityUpgradeOrder, 0, ClassStats{}, nil)
	if err != nil || result == nil || result.Pieces[1].Name != "helmet" {
		t.Fatalf("no-fill equipment result = %#v, %v", result, err)
	}
}

func TestRepeatedOptimizationKeepsPriorityResult(t *testing.T) {
	items := make([]Item, 0, len(slotOrder)*2)
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		for _, variant := range []struct {
			name   string
			attack float64
		}{{"low", 1}, {"high", 2}} {
			item := Item{ID: slot + "-" + variant.name, Name: slot + " " + variant.name, MainCategory: category, SubName: slot, Grade: 1, Attributes: map[string]interface{}{"attack": variant.attack}}
			if slot == "weapon" {
				item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
			}
			items = append(items, item)
		}
	}
	for range 5 {
		result, err := optimizeConfigured(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 1, 1, nil, false, map[string]int{}, rarityUpgradeOrder, 0)
		if err != nil || result == nil || !result.Possible {
			t.Fatalf("stable optimization result = %#v, %v", result, err)
		}
		for _, piece := range result.Pieces {
			if piece.Name != piece.Slot+" high" {
				t.Fatalf("priority selected %q for %s", piece.Name, piece.Slot)
			}
		}
	}
}

func TestStatPriorityChecksAllRarityCandidates(t *testing.T) {
	items := make([]Item, 0, len(slotOrder)*2)
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		for _, variant := range []struct {
			grade  int
			attack float64
		}{{1, 1}, {2, 100}} {
			item := Item{ID: fmt.Sprintf("%s-%d", slot, variant.grade), Name: slot, MainCategory: category, SubName: slot, Grade: variant.grade, Attributes: map[string]interface{}{"attack": variant.attack}}
			if slot == "weapon" {
				item.Attributes["weaponDamage"] = 1
				item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
			}
			items = append(items, item)
		}
	}
	result, err := optimizeConfigured(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 1, 2, nil, false, map[string]int{}, rarityUpgradeOrder, 0, [4]int{0, 1, 2, 3})
	if err != nil || result == nil || result.LevelCombination[0] != 1 {
		t.Fatalf("stat-priority rarity result = %#v, %v", result, err)
	}
}

func TestEqualRarityCombinationsUseStats(t *testing.T) {
	items := make([]Item, 0, len(slotOrder)+2)
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		item := Item{Name: slot, Grade: 1, MainCategory: category, SubName: slot}
		if slot == "weapon" {
			item.Attributes = map[string]interface{}{"weaponDamage": 1}
		}
		items = append(items, item)
	}
	items = append(items,
		Item{Name: "weapon high", Grade: 2, MainCategory: "weapon", SubName: "weapon", Attributes: map[string]interface{}{"weaponDamage": 100}, Equipment: EquipmentData{Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
		Item{Name: "clothes high", Grade: 2, MainCategory: "armor", SubName: "clothes", Equipment: EquipmentData{Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
	)
	result, err := optimizeConfigured(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 1, 2, nil, false, map[string]int{}, rarityUpgradeOrder, 0, [4]int{0, 1, 2, 3})
	want := []int{2, 1, 1, 1, 1, 1, 1, 1}
	if err != nil || result == nil || !result.Possible || !slices.Equal(result.LevelCombination, want) {
		t.Fatalf("equal-rarity result = %#v, %v; want %v", result, err, want)
	}
}

func TestStatPriorityConfiguration(t *testing.T) {
	order, labels, err := statPriorityConfiguration(GUIRequest{StatPriority: []string{"Health", "Weapon Damage", "Attack", "Defense"}})
	if err != nil || order != [4]int{3, 0, 1, 2} || !slices.Equal(labels, []string{"Health", "Weapon Damage", "Attack", "Defense"}) {
		t.Fatalf("stat priority configuration = %v, %v, %v", order, labels, err)
	}
	if _, _, err := statPriorityConfiguration(GUIRequest{StatPriority: []string{"Attack", "Attack", "Health", "Defense"}}); err == nil {
		t.Fatal("duplicate stat priority was accepted")
	}
}

func TestFastTrackMinimum(t *testing.T) {
	for _, test := range []struct {
		total int
		want  int
	}{
		{7, 2}, {8, 3}, {15, 3}, {16, 4}, {23, 4}, {24, 5}, {31, 5}, {32, 6}, {33, 0},
	} {
		if got := fastTrackMinimum(test.total); got != test.want {
			t.Errorf("fastTrackMinimum(%d) = %d, want %d", test.total, got, test.want)
		}
	}
}

func TestSolveStatePrefersDistanceAndStats(t *testing.T) {
	far := solveState{Coverage: []int{0}, Stats: [4]float64{0, 100, 0, 0}, Cost: Cost{Recommended: 1}}
	closer := solveState{Coverage: []int{1}, Stats: [4]float64{0, 0, 0, 0}, Cost: Cost{Recommended: 100}}
	if !closer.betterThan(&far, []int{2}) || far.betterThan(&closer, []int{2}) {
		t.Fatal("distance priority is incorrect")
	}
	expensive := solveState{Coverage: []int{1}, Stats: [4]float64{0, 10, 0, 0}, Cost: Cost{Recommended: 100}}
	cheap := solveState{Coverage: []int{1}, Stats: [4]float64{0, 9, 0, 0}, Cost: Cost{Recommended: 1}}
	if !expensive.betterThan(&cheap, []int{2}) {
		t.Fatal("cost incorrectly outranked stats")
	}
	tierTwo := solveState{Coverage: []int{1}, Cost: Cost{TierDeficit: 0}}
	tierOne := solveState{Coverage: []int{1}, Cost: Cost{TierDeficit: 1}}
	if !tierTwo.betterThan(&tierOne, []int{1}) {
		t.Fatal("higher-tier gem did not break a complete tie")
	}
}

func TestSolveStatePrefersWeaponRarityUpgrade(t *testing.T) {
	priority := [8]int{0}
	weaponFirst := solveState{RaritySum: 25, Rarities: [8]int{4, 3, 3, 3, 3, 3, 3, 3}, RarityPriority: priority, RarityPriorityCount: 1}
	armorFirst := solveState{RaritySum: 25, Rarities: [8]int{3, 3, 3, 4, 3, 3, 3, 3}, RarityPriority: priority, RarityPriorityCount: 1}
	if !weaponFirst.betterThan(&armorFirst, nil) {
		t.Fatal("weapon rarity was not preferred as the first upgrade")
	}
}

func TestArmorCannotUpgradeBeforeWeapon(t *testing.T) {
	items := make([]Item, 0, len(slotOrder)+2)
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		items = append(items, Item{ID: slot + "-green", Name: slot + " green", Grade: 3, MainCategory: category, SubName: slot})
	}
	items = append(items,
		Item{ID: "weapon-blue", Name: "weapon blue", Grade: 4, MainCategory: "weapon", SubName: "weapon", Attributes: map[string]interface{}{"weaponDamage": 100}},
		Item{ID: "clothes-blue", Name: "clothes blue", Grade: 4, MainCategory: "armor", SubName: "clothes", Equipment: EquipmentData{Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
	)
	result, err := optimizeConfigured(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 3, 4, nil, false, map[string]int{}, rarityUpgradeOrder, 0)
	if err != nil || result == nil || result.LevelCombination[0] != 4 || result.LevelCombination[2] != 4 {
		t.Fatalf("weapon-first rarity result = %#v, %v", result, err)
	}
}

func TestDominancePruningKeepsOnlySafeStates(t *testing.T) {
	weaker := solveState{Coverage: []int{1, 0}, MinRarity: 3, MaxRarity: 3, RaritySum: 4, Stats: [4]float64{10, 10, 10, 10}, Damage: 2}
	stronger := solveState{Coverage: []int{2, 1}, MinRarity: 3, MaxRarity: 3, RaritySum: 4, Stats: [4]float64{11, 10, 12, 10}, Damage: 3}
	otherBand := stronger
	otherBand.MinRarity, otherBand.MaxRarity = 2, 3
	if !stronger.dominates(weaker) || weaker.dominates(stronger) || stronger.dominates(otherBand) {
		t.Fatal("dominance comparison is incorrect")
	}

	states := newBoundedSolveStates([]int{2, 1}, 0)
	states.add(solveStateKey(weaker.Coverage, weaker.MinRarity, weaker.MaxRarity), weaker)
	states.add(solveStateKey(stronger.Coverage, stronger.MinRarity, stronger.MaxRarity), stronger)
	states.add(solveStateKey(otherBand.Coverage, otherBand.MinRarity, otherBand.MaxRarity), otherBand)
	states.pruneDominated()
	if _, ok := states.states[solveStateKey(weaker.Coverage, weaker.MinRarity, weaker.MaxRarity)]; ok || len(states.states) != 2 {
		t.Fatalf("dominated states = %#v", states.states)
	}
	capped := newBoundedSolveStates([]int{2, 1}, 1)
	capped.add(solveStateKey(weaker.Coverage, weaker.MinRarity, weaker.MaxRarity), weaker)
	capped.add(solveStateKey(stronger.Coverage, stronger.MinRarity, stronger.MaxRarity), stronger)
	if len(capped.states) != 1 || capped.states[solveStateKey(stronger.Coverage, stronger.MinRarity, stronger.MaxRarity)].Coverage[0] != 2 {
		t.Fatalf("capped dominance = %#v", capped.states)
	}
}

func TestSolveStateIgnoresExtraAffixLevels(t *testing.T) {
	requirements := []Requirement{
		{Key: "aegis", Level: 3, Thresholds: []int{3}},
		{Key: "valor", Level: 3, Thresholds: []int{5}},
	}
	targets, caps := []int{3, 3}, []int{7, 7}
	extraAegis := solveState{Coverage: []int{7, 3}, Targets: targets, Requirements: requirements}
	valorThreshold := solveState{Coverage: []int{3, 5}, Targets: targets, Requirements: requirements}
	if valorThreshold.betterThan(&extraAegis, caps) || extraAegis.betterThan(&valorThreshold, caps) {
		t.Fatal("extra affix levels changed the stat tie")
	}
}

func TestMixedRarityFollowsPriorityAndStaysAdjacent(t *testing.T) {
	items := []Item{}
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		for grade := 1; grade <= 3; grade++ {
			item := Item{Name: slot, Grade: grade, MainCategory: category, SubName: slot}
			if slot == "clothes" && grade == 3 {
				item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
			}
			items = append(items, item)
		}
	}
	result, err := optimize(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 0, 1, 3, nil, false)
	want := []int{3, 2, 3, 2, 2, 2, 2, 2}
	if err != nil || result == nil || !result.Possible || result.Closest || !slices.Equal(result.LevelCombination, want) {
		t.Fatalf("mixed rarity result = %#v, %v; want %v", result, err, want)
	}
	relaxed, err := optimizeConfigured(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1}}, 1, 3, &optimizationProgress{disableRarityDifferenceConstraint: true}, false, nil, rarityUpgradeOrder, 0)
	if err != nil || relaxed == nil || !relaxed.Possible || !slices.Equal(relaxed.LevelCombination, []int{1, 1, 3, 1, 1, 1, 1, 1}) {
		t.Fatalf("relaxed rarity result = %#v, %v", relaxed, err)
	}
}

func TestFixedRaritiesMustBeAdjacent(t *testing.T) {
	_, err := rarityConfiguration(GUIRequest{FixedRarities: map[string]string{"weapon": "Blue", "helmet": "Gold"}}, 4, 6)
	if err == nil || err.Error() != `Hard constraints violated: The difference between "Weapon" and "Helmet" is higher than 1 tier` {
		t.Fatalf("fixed rarity spread error = %v", err)
	}
	fixed, err := rarityConfiguration(GUIRequest{DisableItemRarityDifferenceConstraint: true, FixedRarities: map[string]string{"weapon": "Blue", "helmet": "Gold"}}, 4, 6)
	if err != nil || fixed["weapon"] != 4 || fixed["helmet"] != 6 {
		t.Fatalf("relaxed fixed rarity spread = %#v, %v", fixed, err)
	}
}

func TestClosestCombinationMinimizesShortfall(t *testing.T) {
	items := make([]Item, 0, len(slotOrder))
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		item := Item{Name: slot, Grade: 1, MainCategory: category, SubName: slot}
		if slot == "weapon" {
			item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
		}
		items = append(items, item)
	}
	result, err := optimize(items, nil, []Requirement{{Key: "aegis", Name: "Aegis", Level: 2}}, 0, 1, 1, nil, false)
	if err != nil || result == nil || !result.Possible || !result.Closest || result.Distance != 1 || len(result.Pieces) != len(slotOrder) {
		t.Fatalf("closest result = %#v, %v", result, err)
	}
}

func TestRarityUpperBoundUsesSelectedTiers(t *testing.T) {
	equipment := []Item{}
	for _, slot := range slotOrder {
		category := "armor"
		if slot == "weapon" {
			category = "weapon"
		}
		for grade := 1; grade <= 2; grade++ {
			item := Item{Grade: grade, MainCategory: category, SubName: slot}
			if slot == "weapon" && grade == 2 {
				item.Equipment.Affixes = []Affix{{Name: "Aegis", Level: 1}}
			}
			equipment = append(equipment, item)
		}
	}
	limits := []int{1}
	bounds := buildUpperBounds(equipment, nil, map[string]int{"aegis": 0}, limits)
	low, high := map[string]int{}, map[string]int{}
	for _, slot := range slotOrder {
		low[slot], high[slot] = 1, 1
	}
	high["weapon"] = 2
	if canMeetRequirements(bounds, low, limits) || !canMeetRequirements(bounds, high, limits) {
		t.Fatal("rarity upper bound ignored selected weapon tier")
	}
}

func TestUnusedGemSlotsIncludesOptionalGems(t *testing.T) {
	gem := &GemRef{Affixes: []Affix{{Name: "Aegis", Level: 1}}}
	result := &Solution{
		Effects: map[string]int{"Aegis": 2},
		Pieces: []Piece{{
			NativeAffixes: []Affix{{Name: "Aegis", Level: 1}},
			GemSlots:      []GemSlot{{Gem: gem}, {Gem: gem}, {}},
		}},
	}
	if unused := unusedGemSlots(result); unused != 2 {
		t.Fatalf("unused slots = %d", unused)
	}
}

func TestUnusedAffixSlotsUsesGemTier(t *testing.T) {
	result := &Solution{
		Effects: map[string]int{"Aegis": 2},
		Pieces: []Piece{{
			NativeAffixes: []Affix{{Name: "Aegis", Level: 1}},
			GemSlots: []GemSlot{
				{Tier: 2, Gem: &GemRef{Affixes: []Affix{{Name: "Aegis", Level: 1}, {Name: "Valor", Level: 1}}}},
				{Tier: 1},
			},
		}},
	}
	if unused := unusedAffixSlots(result); unused != 2 {
		t.Fatalf("unused affix slots = %d", unused)
	}
}

func TestFillGemSlotsPrefersTargetAffixes(t *testing.T) {
	result := &Solution{
		Effects:      map[string]int{"Aegis": 1},
		AveragePrice: 10,
		GemCount:     1,
		Pieces: []Piece{{
			NativeAffixes: []Affix{{Name: "Aegis", Level: 1}},
			Gems:          []interface{}{map[string]interface{}{"id": "target"}, nil, nil},
			GemSlots:      []GemSlot{{Type: "Agate", Tier: 1, Gem: &GemRef{ID: "target", Affixes: []Affix{{Name: "Aegis", Level: 1}}}}, {Type: "Agate", Tier: 1}, {Type: "Agate", Tier: 1}},
		}},
	}
	gems := []Item{
		{ID: "bonus", RecommendedPrice: 1, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Valor", Level: 1}}}},
		{ID: "target", RecommendedPrice: 10, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
	}
	if err := fillGemSlots(result, gems, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1, Max: 3, Thresholds: []int{3}}}); err != nil || result.Pieces[0].GemSlots[0].Filled || !result.Pieces[0].GemSlots[1].Filled || !result.Pieces[0].GemSlots[2].Filled || result.Pieces[0].GemSlots[1].Gem.ID != "target" || result.Pieces[0].GemSlots[2].Gem.ID != "bonus" || result.AveragePrice != 21 {
		t.Fatalf("filled result = %#v", result)
	}
}

func TestFillGemSlotsPrefersTargetsBeforeBonus(t *testing.T) {
	result := &Solution{Pieces: []Piece{{NativeAffixes: []Affix{{Name: "Aegis", Level: 1}}, Gems: []interface{}{nil}, GemSlots: []GemSlot{{Type: "Agate", Tier: 1}}}}}
	gems := []Item{
		{ID: "overshoot", RecommendedPrice: 1, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Aegis", Level: 2}}}},
		{ID: "bonus", RecommendedPrice: 2, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Valor", Level: 1}}}},
	}
	if err := fillGemSlots(result, gems, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1, Max: 7, Thresholds: []int{2}}}); err != nil || result.Pieces[0].GemSlots[0].Gem.ID != "overshoot" {
		t.Fatalf("threshold fill = %#v, %v", result, err)
	}
}

func TestFillGemSlotsAllowsCappedTargetGem(t *testing.T) {
	result := &Solution{Pieces: []Piece{{NativeAffixes: []Affix{{Name: "Burst", Level: 5}}, Gems: []interface{}{nil}, GemSlots: []GemSlot{{Type: "Peridot", Tier: 1}}}}}
	gems := []Item{{ID: "burst", Gem: GemData{AffixGemType: 4, AffixGemLevel: 1, Affixes: []Affix{{Name: "Burst", Level: 1}}}}}
	if err := fillGemSlots(result, gems, []Requirement{{Key: "burst", Name: "Burst", Level: 1, Max: 5}}); err != nil || result.Pieces[0].GemSlots[0].Gem == nil {
		t.Fatalf("capped target fill = %#v, %v", result, err)
	}
}

func TestExecuteReturnsFilledBuild(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Execute(GUIRequest{
		CharacterClass: "Mercenary", WeaponClass: "Sword and Shield", MinRarity: "Purple", MaxRarity: "Gold",
		Affixes: []GUIAffix{{Name: "Burst", Level: 1}}, MatchTargetStrictly: false,
		StatPriority: []string{"Health", "Weapon Damage", "Defense", "Attack"},
	})
	tenacious := 0
	if len(result.Sets) == 1 {
		for _, affix := range result.Sets[0].Affixes {
			if affix.Name == "Tenacious" {
				tenacious = affix.Result
			}
		}
	}
	if err != nil || !result.Possible || len(result.Sets) != 1 || tenacious != 7 {
		t.Fatalf("filled execute result = %#v, %v", result, err)
	}
}

func TestStatFirstDoesNotFillBlockedAffix(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass: "Withered Knight", WeaponClass: "Greatsword", MinRarity: "White", MaxRarity: "Gold",
		Affixes:   []GUIAffix{{Name: "Valor", Level: 7}, {Name: "Wrath", Blocked: true}},
		StatFirst: true, MatchTargetStrictly: false,
		StatPriority: []string{"Attack", "Defense", "Weapon Damage", "Health"},
	})
	if err != nil || !result.Possible || len(result.Sets) != 1 {
		t.Fatalf("blocked stat-first result = %#v, %v", result, err)
	}
	strife := 0
	for _, affix := range result.Sets[0].Affixes {
		if strings.EqualFold(affix.Name, "Wrath") && affix.Result > 0 {
			t.Fatalf("blocked affix was filled: %#v", result.Sets[0].Affixes)
		}
		if strings.EqualFold(affix.Name, "Strife") {
			strife = affix.Result
		}
	}
	if strife == 0 {
		t.Fatalf("attack-priority damage affix was not selected: %#v", result.Sets[0].Affixes)
	}
}

func TestFillGemSlotsRaisesTargetsAfterThresholds(t *testing.T) {
	result := &Solution{Pieces: []Piece{{NativeAffixes: []Affix{{Name: "Aegis", Level: 1}}, Gems: []interface{}{map[string]interface{}{"id": "existing"}, nil}, GemSlots: []GemSlot{{Type: "Agate", Tier: 1, Gem: &GemRef{ID: "existing", Affixes: []Affix{{Name: "Aegis", Level: 1}}}}, {Type: "Agate", Tier: 1}}}}}
	gems := []Item{
		{ID: "bonus", RecommendedPrice: 1, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Valor", Level: 1}}}},
		{ID: "target", RecommendedPrice: 10, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
	}
	if err := fillGemSlots(result, gems, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1, Max: 3, Thresholds: []int{2}}}); err != nil || result.Pieces[0].GemSlots[1].Gem.ID != "target" {
		t.Fatalf("post-threshold fill = %#v, %v", result, err)
	}
}

func TestFillGemSlotsAvoidsLocalThresholdChoice(t *testing.T) {
	result := &Solution{Pieces: []Piece{{NativeAffixes: []Affix{{Name: "Aegis", Level: 1}}, Gems: []interface{}{nil, nil}, GemSlots: []GemSlot{{Type: "Agate", Tier: 1}, {Type: "Agate", Tier: 1}}}}}
	gems := []Item{
		{ID: "bonus", RecommendedPrice: 3, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Valor", Level: 1}}}},
		{ID: "target-one", RecommendedPrice: 1, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Aegis", Level: 1}}}},
		{ID: "target-two", RecommendedPrice: 2, Gem: GemData{AffixGemType: 1, AffixGemLevel: 1, Affixes: []Affix{{Name: "Aegis", Level: 2}}}},
	}
	if err := fillGemSlots(result, gems, []Requirement{{Key: "aegis", Name: "Aegis", Level: 1, Max: 3, Thresholds: []int{3}}}); err != nil {
		t.Fatal(err)
	}
	ids := []string{result.Pieces[0].GemSlots[0].Gem.ID, result.Pieces[0].GemSlots[1].Gem.ID}
	if !(slices.Contains(ids, "target-two") && slices.Contains(ids, "bonus")) {
		t.Fatalf("local fill = %#v", ids)
	}
}

func TestEffectThresholds(t *testing.T) {
	levels := map[string]string{"1": "A.", "2": "A.", "3": "A. B.", "4": "A. B.", "5": "A. B. C."}
	if got := effectThresholds(levels); !slices.Equal(got, []int{3, 5}) {
		t.Fatalf("thresholds = %v", got)
	}
}

func TestFillGemSlotsSavedRequest(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass: "Mercenary", WeaponClass: "Sword and Shield", WeaponRarity: "Any", MinRarity: "Purple", MaxRarity: "Gold", Ring: "HP/Any", Amulet: "HP/Any", MatchTargetStrictly: false,
		Affixes: []GUIAffix{{Name: "Aegis", Level: 3, Wine: 2}, {Name: "Elusive", Level: 3}, {Name: "Fervor", Level: 3, Wine: 2}, {Name: "Resilience", Level: 3, Wine: 1}, {Name: "Stoic", Level: 4, Wine: 1}, {Name: "Valor", Level: 3, Wine: 1}, {Name: "Vitality", Level: 3}},
	})
	if err != nil || !result.Possible || result.Closest {
		t.Fatalf("fill saved request = %#v, %v", result, err)
	}
}

func TestMixedRarityRequestFindsExactSet(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(GUIRequest{
		CharacterClass: "Mercenary", WeaponClass: "Sword and Shield", WeaponRarity: "Any", MinRarity: "Purple", MaxRarity: "Gold", Ring: "HP/Any", Amulet: "HP/Any", MatchTargetStrictly: false,
		Affixes: []GUIAffix{{Name: "Aegis", Level: 5}, {Name: "Elusive", Level: 3}, {Name: "Fervor", Level: 5}, {Name: "Resilience", Level: 4}, {Name: "Stoic", Level: 5}, {Name: "Valor", Level: 5}, {Name: "Vitality", Level: 4}},
	})
	if err != nil || !result.Possible || result.Closest {
		t.Fatalf("mixed rarity result = %#v, %v", result, err)
	}
}

func TestFormatGUIAffixesShowsTargetAndCappedSelection(t *testing.T) {
	request := []GUIAffix{{Name: "Valor", Level: 2}, {Name: "Aegis", Level: 2, Wine: 1}}
	result := &Solution{Pieces: []Piece{{
		NativeAffixes: []Affix{{Name: "Aegis", Level: 4}, {Name: "Stoic", Level: 2}},
		GemSlots:      []GemSlot{{Gem: &GemRef{Affixes: []Affix{{Name: "Valor", Level: 2}, {Name: "Aegis", Level: 3}}}}},
	}}}
	details := map[string]GUIAffixDetails{
		"Aegis": {Levels: map[string]string{"1": "", "2": "", "3": "", "4": "", "5": "", "6": "", "7": ""}},
		"Stoic": {Levels: map[string]string{"1": "", "2": ""}},
		"Valor": {Levels: map[string]string{"1": "", "2": ""}},
	}
	affixes := formatGUIAffixes(request, result, details, nil)
	want := []GUIResultAffix{{Name: "Valor", Result: 2, Target: 2}, {Name: "Aegis", Result: 7, Target: 2, Wine: 1}, {Name: "Stoic", Result: 2}}
	if !slices.Equal(affixes, want) {
		t.Fatalf("affixes = %#v", affixes)
	}
}

func TestFormatGemNameOnlyRemovesGemType(t *testing.T) {
	if name := formatGemName(GemRef{Name: "Fortitude · Fighting Spirit Agate"}); name != "Fortitude · Fighting Spirit" {
		t.Fatalf("formatted gem name = %q", name)
	}
}

func TestLoadAffixDetails(t *testing.T) {
	details, err := loadAffixDetails()
	if err != nil || details["Aegis"].Levels["7"] != "Defense +105. Physical Resistance +2.5%." {
		t.Fatalf("Aegis details = %#v, %v", details["Aegis"], err)
	}
}

func TestLoadAffixCategories(t *testing.T) {
	categories, err := loadAffixCategories()
	if err != nil || categories["Aegis"] != "Defensive" || categories["Burst"] != "Offensive" || categories["Blessing"] != "Utility" {
		t.Fatalf("affix categories = %#v, %v", categories, err)
	}
}

func TestGUIWeaponsMatchClass(t *testing.T) {
	service, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	weapons := service.options.WeaponClasses["Mercenary"]
	if !slices.Contains(weapons, "Sword and Shield") || !slices.Contains(weapons, "Warhammer") || slices.Contains(weapons, "Greatsword") {
		t.Fatalf("Mercenary weapons = %#v", weapons)
	}
	weapons = service.options.WeaponClasses["Withered Knight"]
	if len(weapons) != 2 || !slices.Contains(weapons, "Spear and Shield") || !slices.Contains(weapons, "Greatsword") {
		t.Fatalf("Withered Knight weapons = %#v", weapons)
	}
	_, err = service.Execute(GUIRequest{CharacterClass: "Mercenary", WeaponClass: "Greatsword", MinRarity: "1", MaxRarity: "6", Affixes: []GUIAffix{{Name: "Aegis", Level: 1}}})
	if err == nil || err.Error() != `weapon class "Greatsword" is not available to Mercenary` {
		t.Fatalf("unavailable weapon error = %v", err)
	}
}

func TestFilterEquipmentAcceptsIndependentRingFilters(t *testing.T) {
	items := []Item{
		{ID: "hp-phys", SubName: "Ring", Attributes: map[string]interface{}{"maxHealth": 1, "physicalReduction": 1}},
		{ID: "hp-mag", SubName: "Ring", Attributes: map[string]interface{}{"maxHealth": 1, "magicalReduction": 1}},
		{ID: "atk-mag", SubName: "Ring", Attributes: map[string]interface{}{"attack": 1, "magicalIncrease": 1}},
	}

	hp, err := filterEquipment(items, "HP/Any", "")
	if err != nil || len(hp) != 2 || hp[0].ID != "hp-phys" || hp[1].ID != "hp-mag" {
		t.Fatalf("HP/Any = %#v, %v", hp, err)
	}
	mag, err := filterEquipment(items, "Any/MAG", "")
	if err != nil || len(mag) != 2 || mag[0].ID != "hp-mag" || mag[1].ID != "atk-mag" {
		t.Fatalf("Any/MAG = %#v, %v", mag, err)
	}
	any, err := filterEquipment(items, "Any/Any", "")
	if err != nil || len(any) != len(items) {
		t.Fatalf("Any/Any = %#v, %v", any, err)
	}
}

func TestExecuteValidatesWine(t *testing.T) {
	details, err := loadAffixDetails()
	if err != nil {
		t.Fatal(err)
	}
	service := &Engine{options: GUIOptions{AffixDetails: details}}
	if _, err := service.Execute(GUIRequest{MinRarity: "Gold", MaxRarity: "Gray"}); err == nil || err.Error() != "min-rarity cannot exceed max-rarity" {
		t.Fatalf("rarity range error = %v", err)
	}
	if _, err := service.Execute(GUIRequest{MinRarity: "White", MaxRarity: "Purple", WeaponRarity: "Gold"}); err == nil || err.Error() != `Hard constraints violated: "Weapon" rarity is outside the selected rarity range` {
		t.Fatalf("weapon rarity error = %v", err)
	}
	request := GUIRequest{MinRarity: "1", MaxRarity: "6", Affixes: []GUIAffix{{Name: "Aegis", Wine: 3}}}
	if _, err := service.Execute(request); err == nil || err.Error() != "Aegis: wine level must be between 0 and 2" {
		t.Fatalf("wine level error = %v", err)
	}
	request.Affixes = []GUIAffix{{Wine: 2}, {Wine: 2}, {Wine: 2}, {Wine: 2}, {Wine: 1}}
	if _, err := service.Execute(request); err == nil || err.Error() != "total wine levels cannot exceed 8" {
		t.Fatalf("wine total error = %v", err)
	}
	request.Affixes = []GUIAffix{{Name: "Aegis", Level: 0, Wine: 2, Enabled: true}}
	if requirements, err := parseRequirements(nil); err != nil || len(requirements) != 0 {
		t.Fatalf("unselected equipment affix requirements = %#v, %v", requirements, err)
	}
	request.Affixes = []GUIAffix{{Name: "Aegis", Level: 8}}
	if _, err := service.Execute(request); err == nil || err.Error() != "Aegis: level must be between 0 and 7" {
		t.Fatalf("equipment level error = %v", err)
	}
	request.Affixes = []GUIAffix{{Name: "Resilience", Level: 6}}
	if _, err := service.Execute(request); err == nil || err.Error() != "Resilience: level must be between 0 and 5" {
		t.Fatalf("affix-specific level error = %v", err)
	}
}
