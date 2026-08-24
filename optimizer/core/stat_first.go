package core

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	DEBUG                              = false
	defaultStatFirstCandidateLimit     = 20
	statFirstLowGainCount              = 3
	defensePenetrationAttackMultiplier = 3.0
)

func optimizationWorkerCount(parallel bool) int {
	if !parallel {
		return 1
	}
	return max(1, runtime.GOMAXPROCS(0))
}

type statFirstCandidateState struct {
	levels             map[string]int
	stats              [4]float64
	damage             float64
	defensePenetration float64
	thresholds         int
	total              int
}

type statFirstCandidate struct {
	affixes            []GUIAffix
	total              int
	stats              [4]float64
	damage             float64
	defensePenetration float64
	thresholds         int
}

func effectClauses(text string) map[string]bool {
	clauses := map[string]bool{}
	for _, clause := range strings.Split(text, ".") {
		clause = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsSpace(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, clause)
		clause = strings.Join(strings.Fields(clause), " ")
		if clause != "" {
			clauses[clause] = true
		}
	}
	return clauses
}

func affixThresholdCount(detail GUIAffixDetails, level int) int {
	count := 0
	for current := 2; current <= level; current++ {
		currentClauses := effectClauses(detail.Levels[strconv.Itoa(current)])
		previousClauses := effectClauses(detail.Levels[strconv.Itoa(current-1)])
		for clause := range currentClauses {
			if !previousClauses[clause] {
				count++
			}
		}
	}
	return count
}

func (model *statModel) affixThresholdCount(name string, level int) int {
	key := normalize(name)
	if thresholds := model.thresholds[key]; thresholds != nil {
		if count, ok := thresholds[level]; ok {
			return count
		}
	}
	return affixThresholdCount(model.details[key], level)
}

type statFirstResult struct {
	result          GUIResult
	price           float64
	score           float64
	candidateNumber int
}

func compareCentralizedLevels(candidate, current map[string]int) int {
	levels := func(source map[string]int) []int {
		result := make([]int, 0, len(source))
		for _, level := range source {
			if level > 0 {
				result = append(result, level)
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(result)))
		return result
	}
	candidateLevels, currentLevels := levels(candidate), levels(current)
	if len(candidateLevels) != len(currentLevels) {
		if len(candidateLevels) < len(currentLevels) {
			return 1
		}
		return -1
	}
	for index := range candidateLevels {
		if candidateLevels[index] != currentLevels[index] {
			if candidateLevels[index] > currentLevels[index] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func compareAttackPriorityStats(candidate [4]float64, candidateDamage float64, current [4]float64, currentDamage float64, order [4]int) int {
	return compareAttackPriorityStatsWithDefensePenetration(candidate, candidateDamage, 0, current, currentDamage, 0, order)
}

func compareAttackPriorityStatsWithDefensePenetration(candidate [4]float64, candidateDamage, candidateDefensePenetration float64, current [4]float64, currentDamage, currentDefensePenetration float64, order [4]int) int {
	if order == [4]int{} {
		order = [4]int{0, 1, 2, 3}
	}
	for _, index := range order {
		candidateValue, currentValue := candidate[index], current[index]
		if index == 1 {
			candidateValue += candidateDamage + candidateDefensePenetration*defensePenetrationAttackMultiplier
			currentValue += currentDamage + currentDefensePenetration*defensePenetrationAttackMultiplier
		}
		if candidateValue != currentValue {
			if candidateValue > currentValue {
				return 1
			}
			return -1
		}
	}
	return 0
}

func (state statFirstCandidateState) copy() statFirstCandidateState {
	levels := make(map[string]int, len(state.levels)+1)
	for name, level := range state.levels {
		levels[name] = level
	}
	state.levels = levels
	return state
}

func statFirstCandidateSignature(levels map[string]int) string {
	names := make([]string, 0, len(levels))
	for name, level := range levels {
		if level > 0 {
			names = append(names, name+"="+strconv.Itoa(level))
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func betterStatFirstCandidate(candidate, current statFirstCandidateState, order [4]int) bool {
	if comparison := compareAttackPriorityStatsWithDefensePenetration(candidate.stats, candidate.damage, candidate.defensePenetration, current.stats, current.damage, current.defensePenetration, order); comparison != 0 {
		return comparison > 0
	}
	if comparison := compareCentralizedLevels(candidate.levels, current.levels); comparison != 0 {
		return comparison > 0
	}
	return statFirstCandidateSignature(candidate.levels) < statFirstCandidateSignature(current.levels)
}

func appendStatFirstCandidate(states map[int][]statFirstCandidateState, candidate statFirstCandidateState, limit int, order [4]int) {
	if limit <= 0 {
		return
	}
	candidates := states[candidate.total]
	index := sort.Search(len(candidates), func(index int) bool {
		return betterStatFirstCandidate(candidate, candidates[index], order)
	})
	if index == len(candidates) && len(candidates) >= limit {
		return
	}
	candidates = append(candidates, statFirstCandidateState{})
	copy(candidates[index+1:], candidates[index:])
	candidates[index] = candidate
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	states[candidate.total] = candidates
}

func statFirstReferenceCost(request GUIRequest) int {
	if request.StatFirstReferenceCost > 0 {
		return request.StatFirstReferenceCost
	}
	return request.StatFirstCostCeiling
}

func (engine *Engine) referenceCandidateTotals(referenceCost, requiredTotal, maxTotal int) map[int]bool {
	if referenceCost <= 0 || len(engine.priceLookup) == 0 {
		return nil
	}
	upper := float64(referenceCost) * 1.1
	allowed := map[int]bool{}
	cheapest, cheapestPrice := -1, math.MaxFloat64
	for total, price := range engine.priceLookup {
		if total < requiredTotal || total > maxTotal || price <= 0 {
			continue
		}
		if price < cheapestPrice {
			cheapest, cheapestPrice = total, price
		}
		if price <= upper {
			allowed[total] = true
		}
	}
	if len(allowed) == 0 && cheapest >= 0 {
		for total := max(requiredTotal, cheapest-2); total <= min(maxTotal, cheapest+2); total++ {
			if price, ok := engine.priceLookup[total]; ok && price > 0 {
				allowed[total] = true
			}
		}
	}
	if requiredTotal > 0 && requiredTotal <= maxTotal {
		allowed[requiredTotal] = true
	}
	return allowed
}

func candidateAffixes(request GUIRequest, levels map[string]int, optional []string) []GUIAffix {
	affixes := make([]GUIAffix, 0, len(levels))
	seen := map[string]bool{}
	for _, requested := range request.Affixes {
		key := normalize(requested.Name)
		if seen[key] || (!requested.Blocked && levels[key] <= 0) {
			continue
		}
		if requested.Blocked {
			requested.Level = 0
			requested.Enabled = false
		} else {
			requested.Level = levels[key]
			requested.Enabled = true
		}
		affixes = append(affixes, requested)
		seen[key] = true
	}
	for _, name := range optional {
		key := normalize(name)
		if seen[key] || levels[key] <= 0 {
			continue
		}
		affixes = append(affixes, GUIAffix{Name: name, Level: levels[key], Enabled: true})
		seen[key] = true
	}
	return affixes
}

func (engine *Engine) generateStatFirstCandidates(request GUIRequest, order [4]int, maxTotal, limit int, parallel bool, reports ...func(GUIProgress)) ([]statFirstCandidate, error) {
	blocked := blockedAffixNames(request.Affixes)
	explicit := map[string]GUIAffix{}
	total := 0
	for _, affix := range request.Affixes {
		if affix.Blocked || affix.Level <= 0 {
			continue
		}
		key := normalize(affix.Name)
		if previous, exists := explicit[key]; exists && previous.Level >= affix.Level {
			continue
		}
		if previous, exists := explicit[key]; exists {
			total -= previous.Level
		}
		explicit[key] = affix
		total += affix.Level
	}
	requiredTotal := max(request.MinimumAffixLevel, total)
	if requiredTotal > maxTotal {
		return nil, fmt.Errorf("stat-first targets require %d equipment affix levels, but the selected piece rarities support %d", requiredTotal, maxTotal)
	}
	targetTotals := engine.referenceCandidateTotals(statFirstReferenceCost(request), requiredTotal, maxTotal)

	model := newStatModel(engine.options.ClassStats[request.CharacterClass], engine.options.AffixDetails, weaponDamageType(request.WeaponClass))
	initial := statFirstCandidateState{levels: map[string]int{}, total: total}
	for name, affix := range explicit {
		maximum := len(engine.options.AffixDetails[affix.Name].Levels)
		if affix.Level > maximum {
			return nil, fmt.Errorf("%s: level must be between 0 and %d", affix.Name, maximum)
		}
		initial.levels[name] = affix.Level
		initial.thresholds += model.affixThresholdCount(name, affix.Level)
		model.addAffixes(&initial.stats, &initial.damage, []Affix{{Name: affix.Name, Level: affix.Level}})
		initial.defensePenetration += model.affixDefensePenetration([]Affix{{Name: affix.Name, Level: affix.Level}})
	}

	optional := make([]string, 0, len(engine.options.Affixes))
	for _, name := range engine.options.Affixes {
		key := normalize(name)
		if key == "" || blocked[key] || explicit[key].Name != "" {
			continue
		}
		if key == "creation" {
			continue
		}
		if allowed, restricted := weaponOnlyAffixes[key]; restricted && !allowed[canonicalWeaponClass(request.WeaponClass)] {
			continue
		}
		if !affixHasRelevantStat(engine.options.AffixDetails, name) {
			continue
		}
		if len(engine.options.AffixDetails[name].Levels) > 0 {
			optional = append(optional, name)
		}
	}

	beamWidth := max(4, limit*4)
	states := map[int][]statFirstCandidateState{initial.total: []statFirstCandidateState{initial}}
	for optionalIndex, name := range optional {
		if len(reports) > 0 && reports[0] != nil {
			reports[0](GUIProgress{Mode: "Stat First", Stage: "Generating candidates", Current: optionalIndex + 1, Total: len(optional)})
		}
		maximum := len(engine.options.AffixDetails[name].Levels)
		work := make([]statFirstCandidateState, 0)
		for _, candidates := range states {
			work = append(work, candidates...)
		}
		workers := min(optimizationWorkerCount(parallel), max(1, len(work)))
		local := make([]map[int][]statFirstCandidateState, workers)
		var wait sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			start := len(work) * worker / workers
			end := len(work) * (worker + 1) / workers
			wait.Add(1)
			go func(worker int, candidates []statFirstCandidateState) {
				defer wait.Done()
				result := map[int][]statFirstCandidateState{}
				for _, candidate := range candidates {
					appendStatFirstCandidate(result, candidate, beamWidth, order)
					for level := 1; level <= maximum; level++ {
						if candidate.total+level > maxTotal {
							break
						}
						added := candidate.copy()
						added.levels[normalize(name)] = level
						added.total += level
						added.thresholds += model.affixThresholdCount(name, level)
						model.addAffixes(&added.stats, &added.damage, []Affix{{Name: name, Level: level}})
						added.defensePenetration += model.affixDefensePenetration([]Affix{{Name: name, Level: level}})
						appendStatFirstCandidate(result, added, beamWidth, order)
					}
				}
				local[worker] = result
			}(worker, work[start:end])
		}
		wait.Wait()
		next := map[int][]statFirstCandidateState{}
		for _, result := range local {
			for _, candidates := range result {
				for _, candidate := range candidates {
					appendStatFirstCandidate(next, candidate, beamWidth, order)
				}
			}
		}
		states = next
	}

	return chooseStatFirstCandidates(states, request, optional, requiredTotal, limit, targetTotals), nil
}

func chooseStatFirstCandidates(states map[int][]statFirstCandidateState, request GUIRequest, optional []string, requiredTotal, limit int, targetTotals map[int]bool) []statFirstCandidate {
	totals := make([]int, 0, len(states))
	for total, candidates := range states {
		if total >= requiredTotal && len(candidates) > 0 && (targetTotals == nil || targetTotals[total]) {
			totals = append(totals, total)
		}
	}
	sort.Ints(totals)
	if len(totals) > limit {
		selected := make([]int, 0, limit)
		for i := 0; i < limit; i++ {
			index := 0
			if limit > 1 {
				index = i * (len(totals) - 1) / (limit - 1)
			}
			if len(selected) == 0 || selected[len(selected)-1] != totals[index] {
				selected = append(selected, totals[index])
			}
		}
		totals = selected
	}

	result := make([]statFirstCandidate, 0, limit)
	selected := map[string]bool{}
	appendCandidate := func(candidate statFirstCandidateState) {
		signature := statFirstCandidateSignature(candidate.levels)
		if selected[signature] || len(result) >= limit {
			return
		}
		selected[signature] = true
		result = append(result, statFirstCandidate{affixes: candidateAffixes(request, candidate.levels, optional), total: candidate.total, stats: candidate.stats, damage: candidate.damage, defensePenetration: candidate.defensePenetration, thresholds: candidate.thresholds})
	}
	for _, total := range totals {
		appendCandidate(states[total][0])
	}
	for _, total := range totals {
		candidates := states[total]
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].thresholds > candidates[j].thresholds
		})
		for _, candidate := range candidates {
			if candidate.thresholds > 0 {
				appendCandidate(candidate)
			}
		}
	}
	for _, total := range totals {
		for _, candidate := range states[total] {
			appendCandidate(candidate)
		}
	}
	return result
}

func sortStatFirstCandidates(candidates []statFirstCandidate, order [4]int) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return compareAttackPriorityStatsWithDefensePenetration(candidates[i].stats, candidates[i].damage, candidates[i].defensePenetration, candidates[j].stats, candidates[j].damage, candidates[j].defensePenetration, order) > 0
	})
}

func statFirstCandidateMilestone(candidate statFirstCandidate, index, total int) string {
	targets := make([]string, 0, len(candidate.affixes))
	for _, affix := range candidate.affixes {
		if affix.Level > 0 {
			targets = append(targets, fmt.Sprintf("%s %d", affix.Name, affix.Level))
		}
	}
	return fmt.Sprintf("Candidate %d/%d: %s", index, total, strings.Join(targets, ", "))
}

func normalizeStatFirstScores(results []statFirstResult, order [4]int) {
	minimum, maximum := [4]float64{}, [4]float64{}
	for index := range minimum {
		value := results[0].result.OptimizationRank.Stats[index]
		if index == 1 {
			value += results[0].result.OptimizationRank.Damage + results[0].result.OptimizationRank.DefensePenetration*defensePenetrationAttackMultiplier
		}
		minimum[index], maximum[index] = value, value
	}
	for _, result := range results[1:] {
		for index := range minimum {
			value := result.result.OptimizationRank.Stats[index]
			if index == 1 {
				value += result.result.OptimizationRank.Damage + result.result.OptimizationRank.DefensePenetration*defensePenetrationAttackMultiplier
			}
			minimum[index] = min(minimum[index], value)
			maximum[index] = max(maximum[index], value)
		}
	}
	for index := range results {
		rank := results[index].result.OptimizationRank
		weight, score := 1.0, 0.0
		for _, stat := range order {
			value := rank.Stats[stat]
			if stat == 1 {
				value += rank.Damage + rank.DefensePenetration*defensePenetrationAttackMultiplier
			}
			score += weight * normalized(value, minimum[stat], maximum[stat])
			weight *= 0.1
		}
		results[index].score = score
	}
}

func estimatedStatFirstCandidateScores(candidates []statFirstCandidate, order [4]int) []float64 {
	results := make([]statFirstResult, len(candidates))
	for index, candidate := range candidates {
		results[index].result.OptimizationRank = &GUIOptimizationRank{Stats: candidate.stats, Damage: candidate.damage, DefensePenetration: candidate.defensePenetration}
	}
	normalizeStatFirstScores(results, order)
	scores := make([]float64, len(results))
	for index, result := range results {
		scores[index] = result.score
	}
	return scores
}

func normalized(value, minimum, maximum float64) float64 {
	if maximum <= minimum {
		return 0
	}
	return (value - minimum) / (maximum - minimum)
}

func statFirstFrontier(results []statFirstResult) []statFirstResult {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].price != results[j].price {
			return results[i].price < results[j].price
		}
		return results[i].score > results[j].score
	})
	frontier := make([]statFirstResult, 0, len(results))
	bestScore := -1.0
	for _, result := range results {
		if result.score <= bestScore {
			continue
		}
		frontier = append(frontier, result)
		bestScore = result.score
	}
	return frontier
}

func filterStatFirstResultsByReferenceCost(results []statFirstResult, referenceCost int) ([]statFirstResult, bool) {
	if referenceCost <= 0 {
		return results, false
	}
	upper := float64(referenceCost) * 1.1
	filtered := make([]statFirstResult, 0, len(results))
	for _, result := range results {
		if result.price <= upper {
			filtered = append(filtered, result)
		}
	}
	if len(filtered) > 0 {
		return filtered, false
	}
	return results, true
}

func selectStatFirstResult(frontier []statFirstResult, minimumGain float64, _ int) statFirstResult {
	selected := frontier[0]
	lowGain := 0
	lastGain := -1.0
	for index, candidate := range frontier[1:] {
		previous := frontier[index]
		priceDelta := candidate.price - previous.price
		gain := 0.0
		if priceDelta > 0 {
			gain = (candidate.score - previous.score) / priceDelta
		}
		lowReturn := minimumGain > 0 && gain < minimumGain
		if minimumGain == 0 && lastGain >= 0 && gain <= lastGain/4 {
			lowReturn = true
		}
		if lowReturn {
			lowGain++
			if lowGain >= statFirstLowGainCount {
				break
			}
		} else {
			lowGain = 0
			selected = candidate
		}
		lastGain = gain
	}
	return selected
}

func statFirstRules(rules []string, candidates, referenceCost int, lookupAvailable, priceRangeFallback bool) []string {
	result := make([]string, 0, len(rules)+4)
	for _, rule := range rules {
		if !strings.HasPrefix(rule, "Optimization mode:") {
			result = append(result, rule)
		}
	}
	result = append(result, fmt.Sprintf("Optimization mode: Stat First, evaluated %d affix combinations", candidates))
	result = append(result, "Stat First stopping rule: automatic 25%-gain, three consecutive low returns")
	result = append(result, "Stat First tie-break: equal gains prefer concentrated affix levels")
	result = append(result, "Stat First Attack equivalence: 1% Defense Penetration = 3% Attack")
	if referenceCost > 0 {
		upper := float64(referenceCost) * 1.1
		message := fmt.Sprintf("Stat First reference cost: %d average price; validated price ceiling %s; lower prices remain eligible", referenceCost, formatNumber(upper))
		if lookupAvailable {
			message += "; candidate levels targeted to lookup prices at or below the ceiling"
		}
		if priceRangeFallback {
			message += "; no validated result was below the ceiling, so all available results were retained"
		}
		result = append(result, message)
	}
	return result
}

func (engine *Engine) executeStatFirst(request GUIRequest, reports ...func(GUIProgress)) (GUIResult, error) {
	started := time.Now()
	var report func(GUIProgress)
	if len(reports) > 0 {
		report = reports[0]
	}
	maximumRarity, err := rarity(request.MaxRarity)
	if err != nil {
		return GUIResult{}, err
	}
	minimumRarity, err := rarity(request.MinRarity)
	if err != nil {
		return GUIResult{}, err
	}
	fixedRarities, err := rarityConfiguration(request, minimumRarity, maximumRarity)
	if err != nil {
		return GUIResult{}, err
	}
	referenceCost := statFirstReferenceCost(request)
	if referenceCost < 0 {
		return GUIResult{}, fmt.Errorf("stat-first reference cost cannot be negative")
	}
	candidateShard, candidateShards := request.StatFirstCandidateShard, request.StatFirstCandidateShards
	if candidateShards <= 0 {
		candidateShards = 1
	}
	if candidateShard < 0 || candidateShard >= candidateShards {
		return GUIResult{}, fmt.Errorf("invalid stat-first candidate shard")
	}
	order, _, err := statPriorityConfiguration(request)
	if err != nil {
		return GUIResult{}, err
	}
	maximumTotal := maxEquipmentAffixLevelsForFixed(maximumRarity, fixedRarities)
	if report != nil {
		report(GUIProgress{Milestone: "Generating Stat First affix candidates."})
	}
	var candidates []statFirstCandidate
	if len(request.StatFirstCandidates) > 0 {
		candidates, err = engine.statFirstCandidatesFromGUI(request, request.StatFirstCandidates)
	} else {
		candidates, err = engine.generateStatFirstCandidates(request, order, maximumTotal, defaultStatFirstCandidateLimit, !request.LowPerformance, report)
	}
	if err != nil {
		return GUIResult{}, err
	}
	if len(candidates) == 0 {
		return GUIResult{}, fmt.Errorf("no stat-first affix candidates meet the configured minimum")
	}
	if report != nil {
		report(GUIProgress{Milestone: fmt.Sprintf("Generated %d candidate affix combinations.", len(candidates))})
	}
	sortStatFirstCandidates(candidates, order)
	candidateSets := make([][]GUIAffix, len(candidates))
	for index, candidate := range candidates {
		candidateSets[index] = candidate.affixes
	}
	if request.StatFirstGenerateOnly {
		return GUIResult{Possible: true, Message: "Stat First candidates generated.", Seconds: time.Since(started).Seconds(), StatFirstCandidateSets: candidateSets}, nil
	}
	prepared, err := engine.prepareStandardDatabase(request, blockedAffixNames(request.Affixes))
	if err != nil {
		return GUIResult{}, err
	}
	if runtime.GOOS == "js" {
		prepared.stats = map[string]cachedItemStats{}
	}
	request.prepared = prepared
	results := make([]statFirstResult, 0, len(candidates))
	alternatives := make([]GUIStatFirstAlternative, 0, len(candidates)+1)
	debugCandidates := make([]GUIDebugCandidate, len(candidates))
	estimatedScores := estimatedStatFirstCandidateScores(candidates, order)
	tested := 0
	baselineRequest := request
	baselineRequest.StatFirst = false
	if report != nil {
		report(GUIProgress{Milestone: "Checking the unconstrained baseline build."})
	}
	if candidateShard == 0 {
		baseline, err := engine.executeStandard(baselineRequest, func(progress GUIProgress) {
			if report != nil && progress.Stage != "" {
				progress.Mode = "Stat First"
				progress.Stage = "Baseline · " + progress.Stage
				report(progress)
			}
		})
		if err != nil {
			return GUIResult{}, err
		}
		tested += baseline.Tested
		if baseline.Possible && !baseline.Closest && baseline.OptimizationRank != nil {
			results = append(results, statFirstResult{result: baseline, price: baseline.OptimizationRank.AveragePrice})
			alternatives = append(alternatives, GUIStatFirstAlternative{Possible: true, Sets: baseline.Sets, OptimizationRank: baseline.OptimizationRank})
		}
	}
	executeCandidate := func(index int, candidateReports ...func(GUIProgress)) (GUIResult, error) {
		candidateRequest := request
		candidateRequest.StatFirst = false
		candidateRequest.Affixes = candidates[index].affixes
		if len(candidateReports) == 0 {
			candidateReports = []func(GUIProgress){func(GUIProgress) {}}
		}
		return engine.executeStandard(candidateRequest, candidateReports...)
	}
	var candidateResults []GUIResult
	var candidateErrors []error
	if runtime.GOOS != "js" && !request.LowPerformance {
		candidateResults = make([]GUIResult, len(candidates))
		candidateErrors = make([]error, len(candidates))
		jobs := make(chan int)
		workers := min(optimizationWorkerCount(true), len(candidates))
		var wait sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				for index := range jobs {
					candidateResults[index], candidateErrors[index] = executeCandidate(index)
				}
			}()
		}
		for index := range candidates {
			if candidateShards <= 1 || index%candidateShards == candidateShard {
				jobs <- index
			}
		}
		close(jobs)
		wait.Wait()
	}
	for index, candidate := range candidates {
		score := estimatedScores[index]
		status := "invalid"
		if candidateShards > 1 && index%candidateShards != candidateShard {
			status = "UNTESTED"
		}
		debugCandidates[index] = GUIDebugCandidate{Number: index + 1, Affixes: candidate.affixes, Status: status, EstimatedStats: candidate.stats, EstimatedDamage: candidate.damage, EstimatedDefensePenetration: candidate.defensePenetration, EstimatedScore: &score}
		if status == "UNTESTED" {
			continue
		}
		if report != nil {
			report(GUIProgress{Mode: "Stat First", Stage: "Candidate", Current: index + 1, Total: len(candidates), Tested: tested})
		}
		var result GUIResult
		if candidateResults != nil {
			result, err = candidateResults[index], candidateErrors[index]
		} else {
			result, err = executeCandidate(index, func(progress GUIProgress) {
				if report != nil && progress.Stage != "" {
					progress.Mode = "Stat First"
					progress.Stage = fmt.Sprintf("Candidate %d/%d · %s", index+1, len(candidates), progress.Stage)
					progress.Tested += tested
					report(progress)
				}
			})
		}
		if err != nil {
			return GUIResult{}, err
		}
		tested += result.Tested
		if report != nil {
			milestone := statFirstCandidateMilestone(candidate, index+1, len(candidates))
			if result.OptimizationRank != nil {
				milestone += fmt.Sprintf(" · Price: %s", formatNumber(result.OptimizationRank.AveragePrice))
			} else {
				milestone += " · no valid set"
			}
			report(GUIProgress{Milestone: milestone})
		}
		debug := &debugCandidates[index]
		if result.OptimizationRank != nil {
			debug.Price = result.OptimizationRank.AveragePrice
			debug.Stats = result.OptimizationRank.Stats
			debug.Damage = result.OptimizationRank.Damage
			debug.DefensePenetration = result.OptimizationRank.DefensePenetration
		}
		if result.Closest {
			debug.Status = "closest"
		} else if result.Possible && result.OptimizationRank != nil {
			debug.Status = "valid"
		}
		if !result.Possible || result.Closest || result.OptimizationRank == nil {
			continue
		}
		results = append(results, statFirstResult{result: result, price: result.OptimizationRank.AveragePrice, candidateNumber: index + 1})
		alternatives = append(alternatives, GUIStatFirstAlternative{CandidateNumber: index + 1, Possible: true, Sets: result.Sets, OptimizationRank: result.OptimizationRank})
	}
	if len(results) == 0 {
		return GUIResult{Possible: false, Message: "No valid Stat First candidate was found.", Tested: tested, Seconds: time.Since(started).Seconds(), Debug: &GUIDebug{Candidates: debugCandidates}, StatFirstCandidateSets: candidateSets}, nil
	}
	if report != nil {
		report(GUIProgress{Mode: "Stat First", Stage: "Scoring results", Current: 1, Total: 1, Tested: tested})
	}
	allScored := append([]statFirstResult(nil), results...)
	normalizeStatFirstScores(allScored, order)
	for _, result := range allScored {
		if result.candidateNumber == 0 {
			continue
		}
		debug := &debugCandidates[result.candidateNumber-1]
		score := result.score
		debug.Score = &score
	}
	results, priceRangeFallback := filterStatFirstResultsByReferenceCost(results, referenceCost)
	if referenceCost > 0 && !priceRangeFallback {
		upper := float64(referenceCost) * 1.1
		for index := range debugCandidates {
			if debugCandidates[index].Status == "valid" && debugCandidates[index].Price > upper {
				debugCandidates[index].Status = "OVER BUDGET"
			}
		}
	}
	normalizeStatFirstScores(results, order)
	frontier := statFirstFrontier(results)
	selected := selectStatFirstResult(frontier, 0, referenceCost)
	for _, result := range results {
		if result.candidateNumber == 0 {
			continue
		}
		debugCandidates[result.candidateNumber-1].Ranked = true
	}
	for _, result := range frontier {
		if result.candidateNumber > 0 {
			debugCandidates[result.candidateNumber-1].Frontier = true
		}
	}
	if selected.candidateNumber > 0 {
		debugCandidates[selected.candidateNumber-1].Selected = true
	}
	response := selected.result
	response.Tested = tested
	response.Seconds = time.Since(started).Seconds()
	response.Rules = statFirstRules(response.Rules, len(candidates), referenceCost, len(engine.priceLookup) > 0, priceRangeFallback)
	response.Debug = &GUIDebug{Candidates: debugCandidates}
	response.StatFirstAlternatives = alternatives
	response.StatFirstCandidateSets = candidateSets
	return response, nil
}

func (engine *Engine) statFirstCandidatesFromGUI(request GUIRequest, sets [][]GUIAffix) ([]statFirstCandidate, error) {
	model := newStatModel(engine.options.ClassStats[request.CharacterClass], engine.options.AffixDetails, weaponDamageType(request.WeaponClass))
	if model == nil {
		return nil, fmt.Errorf("Stat First candidate model is unavailable")
	}
	blocked := blockedAffixNames(request.Affixes)
	result := make([]statFirstCandidate, 0, len(sets))
	for _, affixes := range sets {
		candidate := statFirstCandidate{affixes: affixes}
		for _, affix := range affixes {
			if affix.Level <= 0 {
				continue
			}
			key := normalize(affix.Name)
			if blocked[key] {
				return nil, fmt.Errorf("blocked affix %q was included in Stat First candidates", affix.Name)
			}
			detail, ok := model.details[key]
			if !ok || affix.Level > len(detail.Levels) {
				return nil, fmt.Errorf("invalid Stat First candidate affix %q level %d", affix.Name, affix.Level)
			}
			candidate.total += affix.Level
			candidate.thresholds += model.affixThresholdCount(key, affix.Level)
			model.addAffixes(&candidate.stats, &candidate.damage, []Affix{{Name: affix.Name, Level: affix.Level}})
			candidate.defensePenetration += model.affixDefensePenetration([]Affix{{Name: affix.Name, Level: affix.Level}})
		}
		result = append(result, candidate)
	}
	return result, nil
}
