package heroconstants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/batch"
	"github.com/aneviaro/stratz-mcp/internal/graphql/generated"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
	"golang.org/x/sync/singleflight"
)

// Options configures hero constants and statistics execution.
type Options struct {
	Executor            stratz.Executor
	MaxUpstreamRequests int
	MaxBatchSize        int
	// ConstantsTTL enables a process-local in-memory cache of the parsed STRATZ
	// constants aggregate for this duration. A value of zero (the default)
	// disables caching so every loadConstants call fetches upstream, preserving
	// exact request-count semantics for tests. Heroes, items, and abilities are
	// public reference data that changes only with Dota patches, so a long TTL
	// is safe in production.
	ConstantsTTL time.Duration
	Now          func() time.Time
}

// Service executes curated hero constants and statistics operations.
type Service struct {
	executor            stratz.Executor
	maxUpstreamRequests int
	maxBatchSize        int
	constantsTTL        time.Duration
	now                 func() time.Time

	constantsMu    sync.Mutex
	constants      *cachedConstants
	constantsGroup singleflight.Group
}

// New constructs the hero constants service.
func New(options Options) (*Service, error) {
	if options.Executor == nil {
		return nil, errors.New("STRATZ executor is required")
	}
	if options.MaxUpstreamRequests < 1 || options.MaxUpstreamRequests > 5 {
		return nil, errors.New("upstream request budget must be between 1 and 5")
	}
	if options.MaxBatchSize == 0 {
		options.MaxBatchSize = 25
	}
	if options.MaxBatchSize < 1 || options.MaxBatchSize > 25 {
		return nil, errors.New("batch size must be between 1 and 25")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{
		executor:            options.Executor,
		maxUpstreamRequests: options.MaxUpstreamRequests,
		maxBatchSize:        options.MaxBatchSize,
		constantsTTL:        options.ConstantsTTL,
		now:                 options.Now,
	}, nil
}

// FetchHero resolves and returns one normalized hero.
func (s *Service) FetchHero(ctx context.Context, identifier any) (*Result[contracts.Hero], error) {
	response, constants, err := s.loadConstants(ctx, s.budget())
	if err != nil {
		return nil, err
	}
	hero, resolveErr := resolveHero(constants.Heroes, identifier)
	if resolveErr != nil {
		return nil, resolveErr
	}
	return &Result[contracts.Hero]{
		Data:       mapHero(hero),
		Raw:        rawData(response.Data),
		RateLimits: response.RateLimits,
	}, nil
}

// BatchHeroes resolves up to the configured number of heroes atomically.
func (s *Service) BatchHeroes(ctx context.Context, identifiers []any) (*Result[[]contracts.Hero], error) {
	if len(identifiers) == 0 || len(identifiers) > s.maxBatchSize {
		return nil, invalid(fmt.Sprintf("Hero batch size must be between 1 and %d", s.maxBatchSize), nil)
	}
	response, constants, err := s.loadConstants(ctx, s.budget())
	if err != nil {
		return nil, err
	}
	plan, err := batch.NewPlan(identifiers, s.maxBatchSize, canonicalIdentifier)
	if err != nil {
		return nil, invalid("Hero batch input is invalid", map[string]any{"reason": err.Error()})
	}
	results := make(map[string]contracts.Hero, len(plan.Unique()))
	for _, identifier := range plan.Unique() {
		hero, resolveErr := resolveHero(constants.Heroes, identifier)
		if resolveErr != nil {
			resolveErr.FailedInput = map[string]any{"index": firstIndex(identifiers, identifier), "value": identifier}
			return nil, resolveErr
		}
		key, _ := canonicalIdentifier(identifier)
		results[key] = mapHero(hero)
	}
	items, err := batch.Reconstruct(plan, results)
	if err != nil {
		return nil, protocol("Failed to reconstruct the hero batch")
	}
	return &Result[[]contracts.Hero]{
		Data:       items,
		Raw:        rawData(response.Data),
		RateLimits: response.RateLimits,
	}, nil
}

// ResolveHeroID resolves any public hero identifier to its canonical numeric ID.
func (s *Service) ResolveHeroID(ctx context.Context, identifier any) (int64, error) {
	return s.ResolveHeroIDWithBudget(ctx, identifier, s.budget())
}

// HeroNames resolves the localized display name for each requested numeric hero
// ID from a single STRATZ constants request, charging the caller's shared
// per-MCP-call budget. IDs absent from the upstream constants are omitted from
// the returned map (callers treat a missing entry as an unknown name). An empty
// input skips the upstream request entirely and returns an empty map.
func (s *Service) HeroNames(
	ctx context.Context,
	ids []int64,
	budget *stratz.RequestBudget,
) (map[int64]string, []stratz.RateLimit, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil, nil
	}
	response, constants, err := s.loadConstants(ctx, budget)
	if err != nil {
		return nil, nil, err
	}
	wanted := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			wanted[id] = struct{}{}
		}
	}
	names := make(map[int64]string, len(wanted))
	for index := range constants.Heroes {
		hero := &constants.Heroes[index]
		if _, ok := wanted[hero.ID]; !ok {
			continue
		}
		if name := cleanPointer(hero.LocalizedName, 128); name != nil {
			names[hero.ID] = *name
		}
	}
	return names, response.RateLimits, nil
}

// ResolveHeroIDWithBudget resolves a hero while charging the caller's shared
// per-MCP-call request budget.
func (s *Service) ResolveHeroIDWithBudget(
	ctx context.Context,
	identifier any,
	budget *stratz.RequestBudget,
) (int64, error) {
	_, constants, err := s.loadConstants(ctx, budget)
	if err != nil {
		return 0, err
	}
	hero, resolveErr := resolveHero(constants.Heroes, identifier)
	if resolveErr != nil {
		return 0, resolveErr
	}
	return hero.ID, nil
}

// FetchConstants returns the requested normalized constants collection.
func (s *Service) FetchConstants(ctx context.Context, requested string) (*Result[contracts.StratzGetConstantsData], error) {
	if !validConstantType(requested) {
		return nil, invalid("Unsupported constants type", map[string]any{"type": requested})
	}
	response, constants, err := s.loadConstants(ctx, s.budget())
	if err != nil {
		return nil, err
	}
	items, warnings := constantsForType(constants, requested)
	return &Result[contracts.StratzGetConstantsData]{
		Data: contracts.StratzGetConstantsData{
			Type:  requested,
			Items: items,
		},
		Raw:        rawData(response.Data),
		RateLimits: response.RateLimits,
		Warnings:   warnings,
	}, nil
}

// FetchHeroStats returns bounded aggregate statistics for one hero.
func (s *Service) FetchHeroStats(ctx context.Context, filters StatsFilters) (*Result[contracts.StratzGetHeroStatsData], error) {
	budget := s.budget()
	heroID, constantsResponse, err := s.resolveStatsHero(ctx, budget, filters.Hero)
	if err != nil {
		return nil, err
	}
	bucket, effective, rangeErr := translateRange(s.now(), filters.From, filters.To)
	if rangeErr != nil {
		return nil, rangeErr
	}
	if filters.PatchID != nil {
		return nil, invalid("Patch-filtered hero statistics are not supported by the current STRATZ aggregate", nil)
	}
	if filters.Lane != nil {
		return nil, invalid("Lane-filtered hero statistics are not supported by the current STRATZ aggregate", nil)
	}
	if filters.IncludeMatchups || filters.IncludeSynergies {
		return nil, invalid("Matchup and synergy expansion is not supported by the current STRATZ aggregate", nil)
	}
	variables := map[string]any{"heroIds": []int64{heroID}}
	if filters.RankBracket != nil {
		rank, ok := heroStatsRank(*filters.RankBracket)
		if !ok {
			return nil, invalid("Unsupported hero statistics rank bracket", map[string]any{"rank_bracket": *filters.RankBracket})
		}
		variables["bracketIds"] = []string{rank}
	}
	if filters.Role != nil {
		positions, ok := heroStatsPositions(*filters.Role)
		if !ok {
			return nil, invalid("Unsupported hero statistics role", map[string]any{"role": *filters.Role})
		}
		variables["positionIds"] = positions
	}
	query, operation := statisticsOperation(bucket)
	response, err := s.execute(ctx, budget, query, operation, variables)
	if err != nil {
		return nil, err
	}
	var envelope statsEnvelope
	if json.Unmarshal(response.Data, &envelope) != nil {
		return nil, protocol("STRATZ returned an invalid hero-statistics payload")
	}
	if envelope.HeroStats == nil {
		return nil, notFound("Hero statistics were not found", map[string]any{"hero_id": heroID})
	}
	stats := aggregateStats(envelope.HeroStats.Stats, effective, heroID)
	if stats == nil {
		return nil, notFound("Hero statistics were not found in the effective date range", map[string]any{"hero_id": heroID})
	}
	warnings := []string{"Pick and ban rates are unavailable from the current STRATZ win aggregate"}
	raw := rawData(response.Data)
	if constantsResponse != nil {
		raw = map[string]any{"constants": rawData(constantsResponse.Data), "statistics": raw}
	}
	data := mapStats(stats, filters)
	return &Result[contracts.StratzGetHeroStatsData]{
		Data:           data,
		Raw:            raw,
		RateLimits:     response.RateLimits,
		Warnings:       warnings,
		EffectiveRange: &effective,
		PatchID:        filters.PatchID,
	}, nil
}

func aggregateStats(rows []upstreamStats, effective DateRange, heroID int64) *upstreamStats {
	result := &upstreamStats{HeroID: heroID}
	found := false
	for _, row := range rows {
		period, ok := heroStatsPeriod(row.Period)
		if !ok || period.Before(effective.From) || !period.Before(effective.To) ||
			(row.HeroID != 0 && row.HeroID != heroID) {
			continue
		}
		result.MatchCount += row.MatchCount
		result.WinCount += row.WinCount
		found = true
	}
	if !found {
		return nil
	}
	return result
}

func heroStatsPeriod(value int64) (time.Time, bool) {
	if value > 100000000000 {
		return time.UnixMilli(value).UTC(), true
	}
	if value > 1000000000 {
		return time.Unix(value, 0).UTC(), true
	}
	text := strconv.FormatInt(value, 10)
	for _, layout := range []string{"20060102", "200601", "2006"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC(), true
		}
	}
	if len(text) == 6 {
		year, yearErr := strconv.Atoi(text[:4])
		week, weekErr := strconv.Atoi(text[4:])
		if yearErr == nil && weekErr == nil && week >= 1 && week <= 53 {
			date := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
			monday := date.AddDate(0, 0, -((int(date.Weekday()) + 6) % 7))
			return monday.AddDate(0, 0, (week-1)*7), true
		}
	}
	return time.Time{}, false
}

func heroStatsRank(value string) (string, bool) {
	rank := strings.ToUpper(strings.TrimSpace(value))
	switch rank {
	case "ANCIENT", "ARCHON", "CRUSADER", "DIVINE", "GUARDIAN", "HERALD", "IMMORTAL", "LEGEND", "UNCALIBRATED":
		return rank, true
	default:
		return "", false
	}
}

func heroStatsPositions(value string) ([]string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "core":
		return []string{"POSITION_1", "POSITION_2", "POSITION_3"}, true
	case "support":
		return []string{"POSITION_4", "POSITION_5"}, true
	case "carry", "position_1":
		return []string{"POSITION_1"}, true
	case "mid", "position_2":
		return []string{"POSITION_2"}, true
	case "offlane", "off_lane", "position_3":
		return []string{"POSITION_3"}, true
	case "soft_support", "position_4":
		return []string{"POSITION_4"}, true
	case "hard_support", "position_5":
		return []string{"POSITION_5"}, true
	default:
		return nil, false
	}
}

func (s *Service) resolveStatsHero(ctx context.Context, budget *stratz.RequestBudget, identifier any) (int64, *stratz.Response, error) {
	if id, ok := numericHeroID(identifier); ok {
		return id, nil, nil
	}
	response, constants, err := s.loadConstants(ctx, budget)
	if err != nil {
		return 0, nil, err
	}
	hero, resolveErr := resolveHero(constants.Heroes, identifier)
	if resolveErr != nil {
		return 0, nil, resolveErr
	}
	return hero.ID, response, nil
}

// cachedConstants is a single-entry in-memory snapshot of the STRATZ constants
// aggregate. It lets the many callers of loadConstants (hero resolve, hero-name
// decoration, the public constants/hero tools) share one upstream fetch for the
// lifetime of the TTL instead of re-hitting STRATZ on every call. The snapshot
// is treated as read-only by all consumers. Rate limits are deliberately not
// cached: they describe a specific request instant (current quota state), not
// durable data, so serving them from a long-lived entry would misrepresent
// fresh provenance. A cache hit therefore reports no rate limits.
type cachedConstants struct {
	data      upstreamConstants
	raw       json.RawMessage
	fetchedAt time.Time
}

func (s *Service) loadConstants(ctx context.Context, budget *stratz.RequestBudget) (*stratz.Response, upstreamConstants, error) {
	if cached := s.readConstantsCache(); cached != nil {
		return &stratz.Response{HTTPStatus: 200, Data: cached.raw}, cached.data, nil
	}
	// With caching enabled, coalesce concurrent cold-cache misses into a single
	// upstream fetch via singleflight. Without this, a burst of callers on a cold
	// cache each miss, each fetch, and each write (no race, but one request per
	// caller). When caching is disabled (ConstantsTTL <= 0) tests expect exact
	// per-call request counts, so fetch directly without coalescing.
	if s.constantsTTL <= 0 {
		return s.fetchConstants(ctx, budget)
	}
	result, err, _ := s.constantsGroup.Do(constantsFlightKey, func() (any, error) {
		// Double-checked fill: a prior leader that just finished may have
		// populated the cache before this flight started.
		if cached := s.readConstantsCache(); cached != nil {
			return &constantsLoadResult{
				response: &stratz.Response{HTTPStatus: 200, Data: cached.raw},
				data:     cached.data,
			}, nil
		}
		response, data, ferr := s.fetchConstants(ctx, budget)
		if ferr != nil {
			return nil, ferr
		}
		return &constantsLoadResult{response: response, data: data}, nil
	})
	if err != nil {
		return nil, upstreamConstants{}, err
	}
	loaded := result.(*constantsLoadResult)
	return loaded.response, loaded.data, nil
}

// constantsFlightKey is the singleflight key for the constants aggregate; there
// is exactly one constants payload per process, so a single key is sufficient.
const constantsFlightKey = "constants"

// constantsLoadResult carries a fetched constants snapshot out of singleflight.
type constantsLoadResult struct {
	response *stratz.Response
	data     upstreamConstants
}

// fetchConstants executes the STRATZ constants aggregate, validates it, and
// writes the snapshot to the in-memory cache when enabled.
func (s *Service) fetchConstants(ctx context.Context, budget *stratz.RequestBudget) (*stratz.Response, upstreamConstants, error) {
	response, err := s.execute(ctx, budget, generated.StratzGetConstants_Operation, "StratzGetConstants", nil)
	if err != nil {
		return nil, upstreamConstants{}, err
	}
	var envelope constantsEnvelope
	if json.Unmarshal(response.Data, &envelope) != nil {
		return nil, upstreamConstants{}, protocol("STRATZ returned an invalid constants payload")
	}
	s.writeConstantsCache(envelope.Constants, response.Data)
	return response, envelope.Constants, nil
}

// readConstantsCache returns the cached constants when the in-memory cache is
// enabled and the entry is fresh. It returns nil when caching is disabled
// (ConstantsTTL <= 0), the cache is cold, or the entry has expired, so callers
// always fall back to a live fetch. On a hit no budget is charged.
func (s *Service) readConstantsCache() *cachedConstants {
	if s.constantsTTL <= 0 {
		return nil
	}
	s.constantsMu.Lock()
	defer s.constantsMu.Unlock()
	if s.constants == nil || s.now().Sub(s.constants.fetchedAt) > s.constantsTTL {
		return nil
	}
	return s.constants
}

func (s *Service) writeConstantsCache(data upstreamConstants, raw json.RawMessage) {
	if s.constantsTTL <= 0 {
		return
	}
	s.constantsMu.Lock()
	defer s.constantsMu.Unlock()
	s.constants = &cachedConstants{
		data:      data,
		raw:       raw,
		fetchedAt: s.now(),
	}
}

func (s *Service) execute(ctx context.Context, budget *stratz.RequestBudget, query, operation string, variables any) (*stratz.Response, error) {
	response, err := s.executor.Execute(ctx, budget, stratz.Request{
		Query: query, OperationName: operation, Variables: variables, Mode: stratz.ModeCurated, AllowRetries: true,
	})
	if err != nil {
		return nil, mapUpstreamError(err)
	}
	if response == nil || len(response.Data) == 0 {
		return nil, protocol("STRATZ returned no curated GraphQL data")
	}
	return response, nil
}

func (s *Service) budget() *stratz.RequestBudget {
	budget, _ := stratz.NewRequestBudget(s.maxUpstreamRequests)
	return budget
}

func resolveHero(heroes []upstreamHero, identifier any) (*upstreamHero, *Error) {
	return newHeroIndex(heroes).resolve(identifier)
}

type heroIndex struct {
	byID            map[int64]*upstreamHero
	byLocalizedName map[string][]*upstreamHero
	bySlug          map[string][]*upstreamHero
}

func newHeroIndex(heroes []upstreamHero) *heroIndex {
	index := &heroIndex{
		byID:            make(map[int64]*upstreamHero, len(heroes)),
		byLocalizedName: make(map[string][]*upstreamHero, len(heroes)),
		bySlug:          make(map[string][]*upstreamHero, len(heroes)),
	}
	for position := range heroes {
		hero := &heroes[position]
		index.byID[hero.ID] = hero
		if hero.LocalizedName != nil {
			key := normalizedName(*hero.LocalizedName)
			if key != "" {
				index.byLocalizedName[key] = append(index.byLocalizedName[key], hero)
			}
		}
		index.bySlug[canonicalHeroSlug(hero)] = append(index.bySlug[canonicalHeroSlug(hero)], hero)
	}
	for _, matches := range index.byLocalizedName {
		sortHeroes(matches)
	}
	for _, matches := range index.bySlug {
		sortHeroes(matches)
	}
	return index
}

func (index *heroIndex) resolve(identifier any) (*upstreamHero, *Error) {
	if id, ok := numericHeroID(identifier); ok {
		if hero := index.byID[id]; hero != nil {
			return hero, nil
		}
		return nil, notFound("Hero was not found", map[string]any{"hero_id": id})
	}
	text, ok := identifier.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil, invalid("Hero identifier must be a positive ID, localized name, or slug", nil)
	}
	needle := normalizedName(text)
	exact := append([]*upstreamHero(nil), index.byLocalizedName[needle]...)
	for _, hero := range index.bySlug[slug(text)] {
		if !containsHero(exact, hero.ID) {
			exact = append(exact, hero)
		}
	}
	sortHeroes(exact)
	switch len(exact) {
	case 1:
		return exact[0], nil
	case 0:
		return nil, notFound("Hero was not found", map[string]any{"hero": text})
	default:
		suggestions := make([]map[string]any, 0, len(exact))
		for _, hero := range exact {
			suggestions = append(suggestions, map[string]any{
				"hero_id": hero.ID, "name": hero.Name, "localized_name": hero.LocalizedName, "slug": canonicalHeroSlug(hero),
			})
		}
		return nil, invalid("Hero name is ambiguous", map[string]any{"suggestions": suggestions})
	}
}

func sortHeroes(heroes []*upstreamHero) {
	sort.SliceStable(heroes, func(i, j int) bool { return heroes[i].ID < heroes[j].ID })
}

func containsHero(heroes []*upstreamHero, id int64) bool {
	for _, hero := range heroes {
		if hero.ID == id {
			return true
		}
	}
	return false
}

func mapHero(source *upstreamHero) contracts.Hero {
	var primaryAttribute, attackType *string
	if source.Stats != nil {
		primaryAttribute = source.Stats.PrimaryAttribute
		attackType = source.Stats.AttackType
	}
	roles := make([]string, 0, len(source.Roles))
	for _, role := range source.Roles {
		if role.RoleID != nil {
			roles = append(roles, *role.RoleID)
		}
	}
	return contracts.Hero{
		HeroID:           source.ID,
		Name:             clean(source.Name, 128),
		Slug:             canonicalHeroSlug(source),
		LocalizedName:    cleanPointer(source.LocalizedName, 128),
		PrimaryAttribute: enumPointer(primaryAttribute, "strength", "agility", "intelligence", "universal"),
		AttackType:       enumPointer(attackType, "melee", "ranged"),
		Roles:            cleanStrings(roles, 16, 64),
	}
}

func constantsForType(constants upstreamConstants, requested string) ([]contracts.ConstantRecord, []string) {
	var items []contracts.ConstantRecord
	warnings := []string{}
	appendConstants := func(kind string, source []upstreamConstant) {
		for _, item := range source {
			metadata := map[string]any{"type": kind}
			localizedName := item.LocalizedName
			if localizedName == nil && item.Language != nil {
				localizedName = item.Language.DisplayName
			}
			items = append(items, contracts.ConstantRecord{
				ID: item.ID.String(), Name: clean(item.Name, 256), LocalizedName: cleanPointer(localizedName, 256), Metadata: metadata,
			})
		}
	}
	appendHeroes := func() {
		for _, hero := range constants.Heroes {
			metadata := map[string]any{
				"type": "heroes", "slug": canonicalHeroSlug(&hero), "roles": strings.Join(mapHero(&hero).Roles, ","),
			}
			if hero.Stats != nil && hero.Stats.PrimaryAttribute != nil {
				metadata["primary_attribute"] = clean(*hero.Stats.PrimaryAttribute, 64)
			}
			if hero.Stats != nil && hero.Stats.AttackType != nil {
				metadata["attack_type"] = clean(*hero.Stats.AttackType, 64)
			}
			items = append(items, contracts.ConstantRecord{
				ID: strconv.FormatInt(hero.ID, 10), Name: clean(hero.Name, 256), LocalizedName: cleanPointer(hero.LocalizedName, 256), Metadata: metadata,
			})
		}
	}
	switch requested {
	case "heroes":
		appendHeroes()
	case "items":
		appendConstants("items", constants.Items)
	case "abilities":
		appendConstants("abilities", constants.Abilities)
	case "game_modes":
		appendConstants("game_modes", constants.GameModes)
	case "regions":
		appendConstants("regions", constants.Regions)
	case "ranks":
		appendConstants("ranks", constants.Ranks)
		if len(constants.Ranks) == 0 {
			warnings = append(warnings, "Rank constants are unavailable from the approved upstream source")
		}
	case "all":
		appendHeroes()
		appendConstants("items", constants.Items)
		appendConstants("abilities", constants.Abilities)
		appendConstants("game_modes", constants.GameModes)
		appendConstants("regions", constants.Regions)
		appendConstants("ranks", constants.Ranks)
		if len(constants.Ranks) == 0 {
			warnings = append(warnings, "Rank constants are unavailable from the approved upstream source")
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, _ := items[i].Metadata["type"].(string)
		right, _ := items[j].Metadata["type"].(string)
		if left != right {
			return left < right
		}
		return items[i].ID < items[j].ID
	})
	if len(items) > 20000 {
		items = items[:20000]
		warnings = append(warnings, "Constants output was truncated to 20000 records")
	}
	return items, warnings
}

func mapStats(source *upstreamStats, filters StatsFilters) contracts.StratzGetHeroStatsData {
	result := contracts.StratzGetHeroStatsData{
		HeroID: source.HeroID, SampleSize: maxZero(source.MatchCount),
		PickRate: rate(source.PickCount, source.PopulationMatchCount),
		WinRate:  rate(source.WinCount, source.MatchCount),
		BanRate:  rate(source.BanCount, source.PopulationMatchCount),
		Roles:    []contracts.HeroBreakdown{}, Lanes: []contracts.HeroBreakdown{},
		Matchups: []contracts.HeroRelation{}, Synergies: []contracts.HeroRelation{},
	}
	for _, value := range source.Roles {
		result.Roles = append(result.Roles, mapBreakdown(value))
	}
	for _, value := range source.Lanes {
		result.Lanes = append(result.Lanes, mapBreakdown(value))
	}
	if filters.IncludeMatchups {
		result.Matchups = mapRelations(source.Matchups)
	}
	if filters.IncludeSynergies {
		result.Synergies = mapRelations(source.Synergies)
	}
	return result
}

func mapBreakdown(source upstreamBreakdown) contracts.HeroBreakdown {
	return contracts.HeroBreakdown{
		Name: clean(source.Name, 64), SampleSize: maxZero(source.MatchCount),
		PickRate: rate(source.PickCount, source.PopulationMatchCount), WinRate: rate(source.WinCount, source.MatchCount),
	}
}

func mapRelations(source []upstreamRelation) []contracts.HeroRelation {
	if len(source) > 50 {
		source = source[:50]
	}
	result := make([]contracts.HeroRelation, 0, len(source))
	for _, relation := range source {
		winRate := rate(relation.WinCount, relation.MatchCount)
		var advantage *float64
		if winRate != nil && relation.ExpectedWinRate != nil {
			value := clamp(*winRate-*relation.ExpectedWinRate, -1, 1)
			advantage = &value
		}
		result = append(result, contracts.HeroRelation{
			HeroID: relation.HeroID, SampleSize: maxZero(relation.MatchCount), WinRate: winRate, Advantage: advantage,
		})
	}
	return result
}

func translateRange(now time.Time, from, to *time.Time) (string, DateRange, *Error) {
	end := now.UTC()
	if to != nil {
		end = to.UTC()
	}
	start := end.AddDate(0, 0, -7)
	if from != nil {
		start = from.UTC()
	}
	if !start.Before(end) {
		return "", DateRange{}, invalid("Hero statistics range must have from before to", nil)
	}
	days := end.Sub(start).Hours() / 24
	var bucket string
	var effective DateRange
	switch {
	case days <= 31:
		bucket = "day"
		effective = DateRange{From: dayFloor(start), To: dayCeil(end)}
	case days <= 180:
		bucket = "week"
		effective = DateRange{From: weekFloor(start), To: weekCeil(end)}
	default:
		bucket = "month"
		effective = DateRange{From: monthFloor(start), To: monthCeil(end)}
	}
	if effective.To.Sub(effective.From) > 366*24*time.Hour {
		return "", DateRange{}, invalid("Hero statistics effective range cannot exceed 366 days", nil)
	}
	return bucket, effective, nil
}

func statisticsOperation(bucket string) (string, string) {
	switch bucket {
	case "week":
		return generated.StratzGetHeroStatsWeek_Operation, "StratzGetHeroStatsWeek"
	case "month":
		return generated.StratzGetHeroStatsMonth_Operation, "StratzGetHeroStatsMonth"
	default:
		return generated.StratzGetHeroStatsDay_Operation, "StratzGetHeroStatsDay"
	}
}

func dayFloor(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func dayCeil(value time.Time) time.Time {
	floor := dayFloor(value)
	if value.Equal(floor) {
		return floor
	}
	return floor.AddDate(0, 0, 1)
}

func weekFloor(value time.Time) time.Time {
	value = dayFloor(value)
	offset := (int(value.Weekday()) + 6) % 7
	return value.AddDate(0, 0, -offset)
}

func weekCeil(value time.Time) time.Time {
	floor := weekFloor(value)
	if value.Equal(floor) {
		return floor
	}
	return floor.AddDate(0, 0, 7)
}

func monthFloor(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func monthCeil(value time.Time) time.Time {
	floor := monthFloor(value)
	if value.Equal(floor) {
		return floor
	}
	return floor.AddDate(0, 1, 0)
}

func numericHeroID(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), number > 0
	case int64:
		return number, number > 0
	case float64:
		return int64(number), number >= 1 && number == math.Trunc(number)
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil && parsed > 0
	}
	return 0, false
}

func canonicalIdentifier(value any) (string, error) {
	if id, ok := numericHeroID(value); ok {
		return "id:" + strconv.FormatInt(id, 10), nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", errors.New("hero identifier is invalid")
	}
	return "text:" + normalizedName(text), nil
}

func firstIndex(values []any, target any) int {
	key, _ := canonicalIdentifier(target)
	for index, value := range values {
		candidate, _ := canonicalIdentifier(value)
		if candidate == key {
			return index
		}
	}
	return 0
}

func normalizedName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func slug(value string) string {
	var output strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && output.Len() > 0 {
				output.WriteByte('-')
			}
			output.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	return output.String()
}

func heroSlug(value string) string {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "npc_dota_hero_")
	return slug(value)
}

func canonicalHeroSlug(hero *upstreamHero) string {
	if hero.LocalizedName != nil {
		if value := slug(*hero.LocalizedName); value != "" {
			return value
		}
	}
	return heroSlug(hero.Name)
}

func clean(value string, maximum int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	runes := []rune(value)
	if len(runes) > maximum {
		value = string(runes[:maximum])
	}
	return value
}

func cleanPointer(value *string, maximum int) *string {
	if value == nil {
		return nil
	}
	cleaned := clean(*value, maximum)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func cleanStrings(values []string, maximum, length int) []string {
	if len(values) > maximum {
		values = values[:maximum]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := clean(value, length); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func enumPointer(value *string, allowed ...string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	for _, candidate := range allowed {
		if normalized == candidate {
			return &normalized
		}
	}
	return nil
}

func rate(numerator, denominator int64) *float64 {
	if numerator < 0 || denominator <= 0 {
		return nil
	}
	value := clamp(float64(numerator)/float64(denominator), 0, 1)
	return &value
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func maxZero(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func validConstantType(value string) bool {
	switch value {
	case "heroes", "items", "abilities", "game_modes", "regions", "ranks", "all":
		return true
	default:
		return false
	}
}

func addOptional(request map[string]any, key string, value *string) {
	if value != nil {
		request[key] = *value
	}
}

func invalid(message string, details map[string]any) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: contracts.ErrorCodeInvalidArgument, Message: message, Details: details}
}

func notFound(message string, details map[string]any) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: contracts.ErrorCodeNotFound, Message: message, Details: details}
}

func protocol(message string) *Error {
	return &Error{Code: contracts.ErrorCodeUpstreamProtocolError, Message: message, Details: map[string]any{}}
}

func mapUpstreamError(err error) *Error {
	var upstream *stratz.Error
	if errors.As(err, &upstream) {
		return &Error{
			Code: upstream.Code, Message: upstream.Message, Retryable: upstream.Retryable,
			RetryAfter: upstream.RetryAfter, Details: upstream.Details,
		}
	}
	return &Error{Code: contracts.ErrorCodeInternalError, Message: "Curated STRATZ execution failed internally", Details: map[string]any{}}
}
