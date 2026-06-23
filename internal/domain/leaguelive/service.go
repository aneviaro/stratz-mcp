package leaguelive

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/pagination"
	"github.com/aneviaro/stratz-mcp/internal/graphql/generated"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const (
	leagueListOperationVersion  = "leagues/v1"
	leagueMatchOperationVersion = "league-matches/v1"
	liveListOperationVersion    = "live-matches/v1"
)

type Options struct {
	Executor            stratz.Executor
	Token               string
	SchemaVersion       string
	MaxUpstreamRequests int
	Now                 func() time.Time
}

type Service struct {
	executor            stratz.Executor
	token               string
	schemaVersion       string
	maxUpstreamRequests int
	now                 func() time.Time
	cursor              *pagination.Codec
}

func New(options Options) (*Service, error) {
	if options.Executor == nil {
		return nil, errors.New("STRATZ executor is required")
	}
	if strings.TrimSpace(options.Token) == "" {
		return nil, errors.New("cursor token is required")
	}
	if strings.TrimSpace(options.SchemaVersion) == "" {
		return nil, errors.New("schema version is required")
	}
	if options.MaxUpstreamRequests < 1 || options.MaxUpstreamRequests > 5 {
		return nil, errors.New("upstream request budget must be between 1 and 5")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{
		executor: options.Executor, token: options.Token, schemaVersion: options.SchemaVersion,
		maxUpstreamRequests: options.MaxUpstreamRequests, now: options.Now,
		cursor: pagination.NewCodec(pagination.Options{Now: options.Now}),
	}, nil
}

func (service *Service) GetLeague(ctx context.Context, identifier string) (*Result[contracts.League], error) {
	id, domainErr := parseID(identifier, "league_id")
	if domainErr != nil {
		return nil, domainErr
	}
	response, err := service.execute(ctx, service.budget(), generated.StratzGetLeague_Operation, "StratzGetLeague", map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	var envelope leagueEnvelope
	if json.Unmarshal(response.Data, &envelope) != nil {
		return nil, protocol("STRATZ returned an invalid league payload")
	}
	if envelope.League == nil {
		return nil, notFound("League was not found", map[string]any{"league_id": identifier})
	}
	return &Result[contracts.League]{Data: mapLeague(envelope.League, service.now()), Raw: rawData(response.Data), RateLimits: response.RateLimits}, nil
}

func (service *Service) ListLeagues(ctx context.Context, filters LeagueFilters) (*Result[contracts.StratzListLeaguesData], error) {
	if err := validateList(filters.Limit, filters.From, filters.To); err != nil {
		return nil, err
	}
	if filters.Status != nil {
		switch strings.ToLower(strings.TrimSpace(*filters.Status)) {
		case "live", "ongoing", "completed", "ended", "upcoming", "future":
		default:
			return nil, invalid("Unsupported league status", map[string]any{
				"status": *filters.Status,
			})
		}
	}
	filters.Limit = defaultLimit(filters.Limit)
	binding := service.binding("stratz_list_leagues", leagueBinding(filters), filters.Limit, leagueListOperationVersion)
	var state pagination.ScanState[int64]
	if filters.Cursor != "" {
		if _, err := service.cursor.Decode(filters.Cursor, binding, &state); err != nil {
			return nil, cursorError(err)
		}
	}
	budget := service.budget()
	rawPages := []any{}
	var rates []stratz.RateLimit
	pageSize := max(filters.Limit, 20)
	scan, err := pagination.Scan(ctx, pagination.ScanOptions[int64, upstreamLeague]{
		Limit: filters.Limit, MaxPages: service.maxUpstreamRequests, State: stateIf(filters.Cursor, &state),
		Fetch: func(ctx context.Context, skip *int64) (pagination.Page[int64, upstreamLeague], error) {
			offset := pointerValue(skip)
			response, executeErr := service.execute(ctx, budget, generated.StratzListLeagues_Operation, "StratzListLeagues", map[string]any{
				"request": nativeLeagueRequest(filters, pageSize, offset),
			})
			if executeErr != nil {
				return pagination.Page[int64, upstreamLeague]{}, executeErr
			}
			var envelope leaguesEnvelope
			if json.Unmarshal(response.Data, &envelope) != nil {
				return pagination.Page[int64, upstreamLeague]{}, protocol("STRATZ returned an invalid league page")
			}
			rawPages = append(rawPages, rawData(response.Data))
			rates = response.RateLimits
			next := offset + int64(len(envelope.Leagues))
			return pagination.Page[int64, upstreamLeague]{Items: envelope.Leagues, Next: &next, HasMore: len(envelope.Leagues) == pageSize}, nil
		},
		Advance: advanceOffset,
		Accept: func(league upstreamLeague) bool {
			return filters.Query == nil || strings.Contains(strings.ToLower(leagueName(&league)), strings.ToLower(strings.TrimSpace(*filters.Query)))
		},
	})
	if err != nil {
		return nil, err
	}
	items := make([]contracts.League, 0, len(scan.Items))
	for index := range scan.Items {
		items = append(items, mapLeague(&scan.Items[index], service.now()))
	}
	next, err := service.encode(binding, pagination.LifetimeHistorical, scan.Next)
	if err != nil {
		return nil, err
	}
	warnings := incompleteWarning(scan.HasMore && scan.PagesScanned == service.maxUpstreamRequests, "League name search")
	return &Result[contracts.StratzListLeaguesData]{
		Data: contracts.StratzListLeaguesData{Items: items, Page: contracts.PageInfo{NextCursor: next, HasMore: next != nil}},
		Raw:  rawPages, RateLimits: rates, Warnings: warnings,
	}, nil
}

func (service *Service) ListLeagueMatches(ctx context.Context, filters LeagueMatchFilters) (*Result[contracts.StratzListLeagueMatchesData], error) {
	id, domainErr := parseID(filters.LeagueID, "league_id")
	if domainErr != nil {
		return nil, domainErr
	}
	if err := validateList(filters.Limit, filters.From, filters.To); err != nil {
		return nil, err
	}
	filters.Limit = defaultLimit(filters.Limit)
	binding := service.binding("stratz_list_league_matches", leagueMatchBinding(filters), filters.Limit, leagueMatchOperationVersion)
	var offset int64
	if filters.Cursor != "" {
		if _, err := service.cursor.Decode(filters.Cursor, binding, &offset); err != nil {
			return nil, cursorError(err)
		}
	}
	response, err := service.execute(ctx, service.budget(), generated.StratzListLeagueMatches_Operation, "StratzListLeagueMatches", map[string]any{
		"id": id, "request": nativeLeagueMatchRequest(filters, filters.Limit, offset),
	})
	if err != nil {
		return nil, err
	}
	var envelope leagueMatchesEnvelope
	if json.Unmarshal(response.Data, &envelope) != nil {
		return nil, protocol("STRATZ returned an invalid league match page")
	}
	if envelope.League == nil {
		return nil, notFound("League was not found", map[string]any{"league_id": filters.LeagueID})
	}
	items := make([]contracts.MatchSummary, 0, len(envelope.League.Matches))
	for index := range envelope.League.Matches {
		items = append(items, mapSummary(&envelope.League.Matches[index]))
	}
	var next *string
	if len(items) == filters.Limit {
		nextOffset := offset + int64(len(items))
		encoded, encodeErr := service.encode(binding, pagination.LifetimeHistorical, &nextOffset)
		if encodeErr != nil {
			return nil, encodeErr
		}
		next = encoded
	}
	return &Result[contracts.StratzListLeagueMatchesData]{
		Data: contracts.StratzListLeagueMatchesData{Items: items, Page: contracts.PageInfo{NextCursor: next, HasMore: next != nil}},
		Raw:  rawData(response.Data), RateLimits: response.RateLimits,
	}, nil
}

func (service *Service) ListLiveMatches(ctx context.Context, filters LiveFilters) (*Result[contracts.StratzListLiveMatchesData], error) {
	return service.ListLiveMatchesWithBudget(ctx, filters, service.budget())
}

// ListLiveMatchesWithBudget executes the list using the caller's shared
// per-MCP-call request budget.
func (service *Service) ListLiveMatchesWithBudget(
	ctx context.Context,
	filters LiveFilters,
	budget *stratz.RequestBudget,
) (*Result[contracts.StratzListLiveMatchesData], error) {
	if err := validateLive(&filters); err != nil {
		return nil, err
	}
	filters.Limit = defaultLimit(filters.Limit)
	binding := service.binding("stratz_list_live_matches", liveBinding(filters), filters.Limit, liveListOperationVersion)
	var state pagination.ScanState[int64]
	if filters.Cursor != "" {
		if _, err := service.cursor.Decode(filters.Cursor, binding, &state); err != nil {
			return nil, cursorError(err)
		}
	}
	rawPages := []any{}
	var rates []stratz.RateLimit
	pageSize := max(filters.Limit, 20)
	scan, err := pagination.Scan(ctx, pagination.ScanOptions[int64, upstreamLiveMatch]{
		Limit: filters.Limit, MaxPages: service.maxUpstreamRequests, State: stateIf(filters.Cursor, &state),
		Fetch: func(ctx context.Context, skip *int64) (pagination.Page[int64, upstreamLiveMatch], error) {
			offset := pointerValue(skip)
			response, executeErr := service.execute(ctx, budget, generated.StratzListLiveMatches_Operation, "StratzListLiveMatches", map[string]any{
				"request": nativeLiveRequest(filters, pageSize, offset),
			})
			if executeErr != nil {
				return pagination.Page[int64, upstreamLiveMatch]{}, executeErr
			}
			var envelope liveEnvelope
			if json.Unmarshal(response.Data, &envelope) != nil {
				return pagination.Page[int64, upstreamLiveMatch]{}, protocol("STRATZ returned an invalid live-match page")
			}
			rawPages = append(rawPages, rawData(response.Data))
			rates = response.RateLimits
			next := offset + int64(len(envelope.Live.Matches))
			return pagination.Page[int64, upstreamLiveMatch]{Items: envelope.Live.Matches, Next: &next, HasMore: len(envelope.Live.Matches) == pageSize}, nil
		},
		Advance: advanceOffset,
		Accept:  func(match upstreamLiveMatch) bool { return acceptsLive(match, filters) },
	})
	if err != nil {
		return nil, err
	}
	items := make([]contracts.LiveMatch, 0, len(scan.Items))
	for index := range scan.Items {
		items = append(items, mapLive(&scan.Items[index], service.now()))
	}
	next, err := service.encode(binding, pagination.LifetimeLive, scan.Next)
	if err != nil {
		return nil, err
	}
	warnings := []string{}
	if scan.HasMore {
		warnings = append(warnings, "Live results are changing; the continuation resumes a bounded scan and does not represent a complete snapshot")
	}
	return &Result[contracts.StratzListLiveMatchesData]{
		Data: contracts.StratzListLiveMatchesData{Items: items, Page: contracts.PageInfo{NextCursor: next, HasMore: next != nil}},
		Raw:  rawPages, RateLimits: rates, Warnings: warnings,
	}, nil
}

func advanceOffset(offset *int64, consumed int) *int64 {
	value := int64(consumed)
	if offset != nil {
		value += *offset
	}
	return &value
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

func (service *Service) binding(tool string, filters any, size int, version string) pagination.Binding {
	return pagination.Binding{Tool: tool, Filters: filters, PageSize: size, Token: service.token, SchemaVersion: service.schemaVersion, OperationVersion: version}
}

func (service *Service) encode(binding pagination.Binding, lifetime pagination.Lifetime, state any) (*string, error) {
	if state == nil {
		return nil, nil
	}
	value, err := service.cursor.Encode(binding, lifetime, state)
	if err != nil {
		return nil, &Error{Code: contracts.ErrorCodeInternalError, Message: "Failed to create pagination cursor", Details: map[string]any{}}
	}
	return &value, nil
}

func nativeLeagueRequest(filters LeagueFilters, take int, skip int64) map[string]any {
	request := map[string]any{"take": take, "skip": skip}
	if filters.From != nil {
		request["startDateTime"] = filters.From.Unix()
	}
	if filters.To != nil {
		request["endDateTime"] = filters.To.Unix()
	}
	if filters.Tier != nil {
		request["tiers"] = []string{*filters.Tier}
	}
	if filters.Status != nil {
		switch strings.ToLower(*filters.Status) {
		case "live", "ongoing":
			request["isLive"] = true
		case "completed", "ended":
			request["isEnded"] = true
		case "upcoming", "future":
			request["isFuture"] = true
		}
	}
	return request
}

func nativeLeagueMatchRequest(filters LeagueMatchFilters, take int, skip int64) map[string]any {
	request := map[string]any{"take": take, "skip": skip}
	if filters.From != nil {
		request["startDateTime"] = filters.From.Unix()
	}
	if filters.To != nil {
		request["endDateTime"] = filters.To.Unix()
	}
	if filters.PatchID != nil {
		request["gameVersionIds"] = []string{*filters.PatchID}
	}
	return request
}

func nativeLiveRequest(filters LiveFilters, take int, skip int64) map[string]any {
	order := "SPECTATOR_COUNT"
	if filters.Sort == "newest" {
		order = "MATCH_ID"
	}
	request := map[string]any{"take": take, "skip": skip, "orderBy": order}
	if filters.LeagueID != nil {
		request["leagueIds"] = []int64{*filters.LeagueID}
	}
	if filters.HeroID != nil {
		request["heroIds"] = []int64{*filters.HeroID}
	}
	if len(filters.GameStates) > 0 {
		request["gameStates"] = filters.GameStates
	}
	if len(filters.Tiers) > 0 {
		request["leagueTiers"] = filters.Tiers
	}
	return request
}

func acceptsLive(match upstreamLiveMatch, filters LiveFilters) bool {
	if filters.TeamID != nil && !equalID(match.RadiantTeamID, filters.TeamID) && !equalID(match.DireTeamID, filters.TeamID) {
		return false
	}
	if filters.GameModeID != nil &&
		!equalID(enumID(match.GameModeID, gameModeIDs), filters.GameModeID) {
		return false
	}
	if filters.MinimumSpectators != nil && (match.SpectatorCount == nil || *match.SpectatorCount < *filters.MinimumSpectators) {
		return false
	}
	if filters.PlayerID != nil {
		found := false
		for _, player := range match.Players {
			if equalID(player.SteamAccountID, filters.PlayerID) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validateList(limit int, from, to *time.Time) *Error {
	if limit < 0 || limit > 100 {
		return invalid("List limit must be between 1 and 100", nil)
	}
	if from != nil && to != nil && from.After(*to) {
		return invalid("from date must not be after to date", nil)
	}
	return nil
}

func validateLive(filters *LiveFilters) *Error {
	if filters.Limit < 0 || filters.Limit > 100 {
		return invalid("Live match limit must be between 1 and 100", nil)
	}
	if filters.MinimumSpectators != nil && *filters.MinimumSpectators < 0 {
		return invalid("minimum_spectators must not be negative", nil)
	}
	if filters.Sort == "" {
		filters.Sort = "highest_profile"
	}
	if filters.Sort != "highest_profile" && filters.Sort != "newest" {
		return invalid("Unsupported live match sort", nil)
	}
	return nil
}

func parseID(value, field string) (int64, *Error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id < 1 || id > int64(^uint(0)>>1) {
		return 0, invalid(field+" must be a positive integer", nil)
	}
	return id, nil
}

func defaultLimit(limit int) int {
	if limit == 0 {
		return 20
	}
	return limit
}
func pointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
func equalID(left, right *int64) bool { return left != nil && right != nil && *left == *right }
func stateIf[C any](cursor string, state *pagination.ScanState[C]) *pagination.ScanState[C] {
	if cursor == "" {
		return nil
	}
	return state
}
func leagueName(league *upstreamLeague) string {
	if league.DisplayName != nil && strings.TrimSpace(*league.DisplayName) != "" {
		return *league.DisplayName
	}
	return league.Name
}
func incompleteWarning(incomplete bool, label string) []string {
	if !incomplete {
		return nil
	}
	return []string{label + " reached the five-page scan limit; use the continuation cursor to resume"}
}

func leagueBinding(filters LeagueFilters) map[string]any {
	result := map[string]any{}
	if filters.Query != nil {
		result["query"] = *filters.Query
	}
	if filters.Status != nil {
		result["status"] = *filters.Status
	}
	if filters.Tier != nil {
		result["tier"] = *filters.Tier
	}
	if filters.From != nil {
		result["from"] = filters.From.UTC().Format(time.RFC3339)
	}
	if filters.To != nil {
		result["to"] = filters.To.UTC().Format(time.RFC3339)
	}
	return result
}

func leagueMatchBinding(filters LeagueMatchFilters) map[string]any {
	result := map[string]any{"league_id": filters.LeagueID}
	if filters.From != nil {
		result["from"] = filters.From.UTC().Format(time.RFC3339)
	}
	if filters.To != nil {
		result["to"] = filters.To.UTC().Format(time.RFC3339)
	}
	if filters.PatchID != nil {
		result["patch_id"] = *filters.PatchID
	}
	return result
}

func liveBinding(filters LiveFilters) map[string]any {
	result := map[string]any{"sort": filters.Sort, "game_states": filters.GameStates, "tiers": filters.Tiers}
	if filters.PlayerID != nil {
		result["player_id"] = *filters.PlayerID
	}
	if filters.TeamID != nil {
		result["team_id"] = *filters.TeamID
	}
	if filters.LeagueID != nil {
		result["league_id"] = *filters.LeagueID
	}
	if filters.HeroID != nil {
		result["hero_id"] = *filters.HeroID
	}
	if filters.GameModeID != nil {
		result["game_mode_id"] = *filters.GameModeID
	}
	if filters.MinimumSpectators != nil {
		result["minimum_spectators"] = *filters.MinimumSpectators
	}
	return result
}

func rawData(data json.RawMessage) any {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	return value
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
func cursorError(err error) *Error {
	var cursorErr *pagination.Error
	if errors.As(err, &cursorErr) {
		return &Error{Code: cursorErr.Code, Message: cursorErr.Message, Details: cursorErr.Details}
	}
	return &Error{Code: contracts.ErrorCodeInternalError, Message: "Pagination cursor validation failed internally", Details: map[string]any{}}
}
func mapUpstreamError(err error) *Error {
	var upstream *stratz.Error
	if errors.As(err, &upstream) {
		return &Error{Code: upstream.Code, Message: upstream.Message, Retryable: upstream.Retryable, RetryAfter: upstream.RetryAfter, Details: upstream.Details}
	}
	return &Error{Code: contracts.ErrorCodeInternalError, Message: "Curated STRATZ execution failed internally", Details: map[string]any{}}
}
