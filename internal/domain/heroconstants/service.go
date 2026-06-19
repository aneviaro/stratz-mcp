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
	"time"
	"unicode"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/batch"
	"github.com/aneviaro/stratz-mcp/internal/graphql/generated"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const maximumBatchSize = 25

type Options struct {
	Executor            stratz.Executor
	MaxUpstreamRequests int
	Now                 func() time.Time
}

type Service struct {
	executor            stratz.Executor
	maxUpstreamRequests int
	now                 func() time.Time
}

func New(options Options) (*Service, error) {
	if options.Executor == nil {
		return nil, errors.New("STRATZ executor is required")
	}
	if options.MaxUpstreamRequests < 1 || options.MaxUpstreamRequests > 5 {
		return nil, errors.New("upstream request budget must be between 1 and 5")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{
		executor:            options.Executor,
		maxUpstreamRequests: options.MaxUpstreamRequests,
		now:                 options.Now,
	}, nil
}

func (service *Service) GetHero(ctx context.Context, identifier any) (*Result[contracts.Hero], error) {
	response, constants, err := service.loadConstants(ctx, service.budget())
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

func (service *Service) BatchHeroes(ctx context.Context, identifiers []any) (*Result[[]contracts.Hero], error) {
	if len(identifiers) == 0 || len(identifiers) > maximumBatchSize {
		return nil, invalid(fmt.Sprintf("Hero batch size must be between 1 and %d", maximumBatchSize), nil)
	}
	response, constants, err := service.loadConstants(ctx, service.budget())
	if err != nil {
		return nil, err
	}
	plan, err := batch.NewPlan(identifiers, maximumBatchSize, canonicalIdentifier)
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

func (service *Service) GetConstants(ctx context.Context, requested string) (*Result[contracts.StratzGetConstantsData], error) {
	if !validConstantType(requested) {
		return nil, invalid("Unsupported constants type", map[string]any{"type": requested})
	}
	response, constants, err := service.loadConstants(ctx, service.budget())
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

func (service *Service) GetHeroStats(ctx context.Context, filters StatsFilters) (*Result[contracts.StratzGetHeroStatsData], error) {
	budget := service.budget()
	heroID, constantsResponse, err := service.resolveStatsHero(ctx, budget, filters.Hero)
	if err != nil {
		return nil, err
	}
	bucket, effective, rangeErr := translateRange(service.now(), filters.From, filters.To)
	if rangeErr != nil {
		return nil, rangeErr
	}
	if filters.PatchID != nil && bucket != "day" {
		return nil, invalid(
			"Patch-filtered hero statistics are limited to ranges represented by daily buckets",
			map[string]any{"bucket": bucket},
		)
	}
	request := map[string]any{
		"heroId":           heroID,
		"bucket":           bucket,
		"startDateTime":    effective.From.Unix(),
		"endDateTime":      effective.To.Unix(),
		"includeMatchups":  filters.IncludeMatchups,
		"includeSynergies": filters.IncludeSynergies,
	}
	addOptional(request, "gameVersionId", filters.PatchID)
	addOptional(request, "rankBracket", filters.RankBracket)
	addOptional(request, "role", filters.Role)
	addOptional(request, "lane", filters.Lane)
	query, operation := statisticsOperation(bucket)
	response, err := service.execute(ctx, budget, query, operation, map[string]any{"request": request})
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
	warnings := []string{}
	if filters.RankBracket != nil && (envelope.HeroStats.RankDataAvailable == nil || !*envelope.HeroStats.RankDataAvailable) {
		warnings = append(warnings, "Rank-bracket redistribution data is unavailable; rates are omitted to avoid mixing populations")
		envelope.HeroStats.MatchCount = 0
		envelope.HeroStats.PickCount = 0
		envelope.HeroStats.WinCount = 0
		envelope.HeroStats.BanCount = 0
		envelope.HeroStats.PopulationMatchCount = 0
		envelope.HeroStats.Roles = nil
		envelope.HeroStats.Lanes = nil
		envelope.HeroStats.Matchups = nil
		envelope.HeroStats.Synergies = nil
	}
	raw := rawData(response.Data)
	if constantsResponse != nil {
		raw = map[string]any{"constants": rawData(constantsResponse.Data), "statistics": raw}
	}
	return &Result[contracts.StratzGetHeroStatsData]{
		Data:           mapStats(envelope.HeroStats, filters),
		Raw:            raw,
		RateLimits:     response.RateLimits,
		Warnings:       warnings,
		EffectiveRange: &effective,
		PatchID:        filters.PatchID,
	}, nil
}

func (service *Service) resolveStatsHero(ctx context.Context, budget *stratz.RequestBudget, identifier any) (int64, *stratz.Response, error) {
	if id, ok := numericHeroID(identifier); ok {
		return id, nil, nil
	}
	response, constants, err := service.loadConstants(ctx, budget)
	if err != nil {
		return 0, nil, err
	}
	hero, resolveErr := resolveHero(constants.Heroes, identifier)
	if resolveErr != nil {
		return 0, nil, resolveErr
	}
	return hero.ID, response, nil
}

func (service *Service) loadConstants(ctx context.Context, budget *stratz.RequestBudget) (*stratz.Response, upstreamConstants, error) {
	response, err := service.execute(ctx, budget, generated.StratzGetConstants_Operation, "StratzGetConstants", nil)
	if err != nil {
		return nil, upstreamConstants{}, err
	}
	var envelope constantsEnvelope
	if json.Unmarshal(response.Data, &envelope) != nil {
		return nil, upstreamConstants{}, protocol("STRATZ returned an invalid constants payload")
	}
	return response, envelope.Constants, nil
}

func (service *Service) execute(ctx context.Context, budget *stratz.RequestBudget, query, operation string, variables any) (*stratz.Response, error) {
	response, err := service.executor.Execute(ctx, budget, stratz.Request{
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

func (service *Service) budget() *stratz.RequestBudget {
	budget, _ := stratz.NewRequestBudget(service.maxUpstreamRequests)
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
	return contracts.Hero{
		HeroID:           source.ID,
		Name:             clean(source.Name, 128),
		Slug:             canonicalHeroSlug(source),
		LocalizedName:    cleanPointer(source.LocalizedName, 128),
		PrimaryAttribute: enumPointer(source.PrimaryAttribute, "strength", "agility", "intelligence", "universal"),
		AttackType:       enumPointer(source.AttackType, "melee", "ranged"),
		Roles:            cleanStrings(source.Roles, 16, 64),
	}
}

func constantsForType(constants upstreamConstants, requested string) ([]contracts.ConstantRecord, []string) {
	var items []contracts.ConstantRecord
	warnings := []string{}
	appendConstants := func(kind string, source []upstreamConstant) {
		for _, item := range source {
			metadata := map[string]any{"type": kind}
			for _, pair := range item.Metadata {
				if pair.Value != nil && strings.TrimSpace(pair.Key) != "" && len(metadata) < 64 {
					metadata[clean(pair.Key, 64)] = clean(*pair.Value, 256)
				}
			}
			items = append(items, contracts.ConstantRecord{
				ID: item.ID, Name: clean(item.Name, 256), LocalizedName: cleanPointer(item.LocalizedName, 256), Metadata: metadata,
			})
		}
	}
	appendHeroes := func() {
		for _, hero := range constants.Heroes {
			metadata := map[string]any{
				"type": "heroes", "slug": canonicalHeroSlug(&hero), "roles": strings.Join(cleanStrings(hero.Roles, 16, 64), ","),
			}
			if hero.PrimaryAttribute != nil {
				metadata["primary_attribute"] = clean(*hero.PrimaryAttribute, 64)
			}
			if hero.AttackType != nil {
				metadata["attack_type"] = clean(*hero.AttackType, 64)
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
	if end.Sub(start) > 366*24*time.Hour {
		return "", DateRange{}, invalid("Hero statistics range cannot exceed 366 days", nil)
	}
	days := end.Sub(start).Hours() / 24
	switch {
	case days <= 31:
		return "day", DateRange{From: dayFloor(start), To: dayCeil(end)}, nil
	case days <= 180:
		return "week", DateRange{From: weekFloor(start), To: weekCeil(end)}, nil
	default:
		return "month", DateRange{From: monthFloor(start), To: monthCeil(end)}, nil
	}
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
	if len(value) > maximum {
		value = value[:maximum]
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
