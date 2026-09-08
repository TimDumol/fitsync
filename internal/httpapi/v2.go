package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/fitnessrepo"
)

func (a *API) GetMe(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.Authorize(r, ""); err != nil {
		writeAuthorizationError(w, err)
		return
	}
	ctx := r.Context()
	athleteID, householdID := "", ""
	if principal, ok := fitnessauth.PrincipalFromContext(ctx); ok {
		athleteID, householdID = principal.AthleteID, principal.HouseholdID
	} else if scoped, ok := a.auth.(HouseholdAuthorizer); ok {
		var err error
		householdID, err = scoped.HouseholdID(r)
		if err != nil {
			writeAuthorizationError(w, err)
			return
		}
	}
	query := `SELECT id,handle,timezone FROM athletes`
	args := []any{}
	if householdID != "" {
		query += " WHERE household_id=?"
		args = append(args, householdID)
	}
	query += " ORDER BY id"
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	defer rows.Close()
	athletes := []Athlete{}
	for rows.Next() {
		var id, handle, timezone string
		if err := rows.Scan(&id, &handle, &timezone); err != nil {
			writeError(w, http.StatusInternalServerError, "database row failed")
			return
		}
		parsed, err := uuid.Parse(id)
		if err != nil {
			continue
		}
		units := Metric
		athletes = append(athletes, Athlete{AthleteId: openapi_types.UUID(parsed), Handle: handle, Timezone: timezone, Units: &units})
		if athleteID == "" {
			athleteID = id
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "database rows failed")
		return
	}
	if athleteID == "" || len(athletes) == 0 {
		writeError(w, http.StatusUnauthorized, "authenticated athlete is unavailable")
		return
	}
	parsed, err := uuid.Parse(athleteID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authenticated athlete is unavailable")
		return
	}
	selected := athletes[0]
	for _, value := range athletes {
		if value.AthleteId.String() == parsed.String() {
			selected = value
			break
		}
	}
	plans, workouts, sync := true, true, true
	writeJSON(w, http.StatusOK, MeResponse{Athlete: selected, AccessibleAthletes: athletes, Capabilities: struct {
		Plans    *bool `json:"plans,omitempty"`
		Sync     *bool `json:"sync,omitempty"`
		Workouts *bool `json:"workouts,omitempty"`
	}{Plans: &plans, Sync: &sync, Workouts: &workouts}})
}

func (a *API) GetActiveTrainingPlan(w http.ResponseWriter, r *http.Request, id AthleteID) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	value, err := a.repo.ActivePlan(r.Context(), id.String(), true)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	response := PlanResponse{}
	if value.Plan != nil {
		converted, err := apiPlan(value)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		response.Plan = &converted
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) GetTrainingPlanById(w http.ResponseWriter, r *http.Request, id AthleteID, planID PlanID, params GetTrainingPlanByIdParams) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	include := true
	if params.IncludeSteps != nil {
		include = bool(*params.IncludeSteps)
	}
	value, err := a.repo.PlanByID(r.Context(), id.String(), planID.String(), include)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	converted, err := apiPlan(value)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PlanResponse{Plan: &converted})
}

func (a *API) GetPlannedSessions(w http.ResponseWriter, r *http.Request, id AthleteID, params GetPlannedSessionsParams) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	include := true
	if params.IncludeSteps != nil {
		include = bool(*params.IncludeSteps)
	}
	values, err := a.repo.PlannedSessions(r.Context(), id.String(), params.From.String(), params.To.String(), include)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	result := make([]PlannedSession, 0, len(values))
	for _, value := range values {
		converted, err := apiPlannedSession(value)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		result = append(result, converted)
	}
	writeJSON(w, http.StatusOK, PlannedSessionsResponse{Sessions: result})
}

func (a *API) PublishTrainingPlan(w http.ResponseWriter, r *http.Request, id AthleteID, planID PlanID) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	value, err := a.repo.PublishPlan(r.Context(), id.String(), planID.String())
	if err != nil {
		writeRepoError(w, err)
		return
	}
	plan, err := apiPlan(value.Plan)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	var archived *openapi_types.UUID
	if value.ArchivedPlanID != nil {
		parsed, e := uuid.Parse(*value.ArchivedPlanID)
		if e == nil {
			converted := openapi_types.UUID(parsed)
			archived = &converted
		}
	}
	writeJSON(w, http.StatusOK, PublishPlanResponse{Plan: plan, ArchivedPlanId: archived})
}

func (a *API) SkipPlannedSession(w http.ResponseWriter, r *http.Request, id AthleteID, sessionID SessionID) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	value, err := a.repo.SetPlannedSessionStatus(r.Context(), id.String(), sessionID.String(), "skipped")
	if err != nil {
		writeRepoError(w, err)
		return
	}
	converted, err := apiPlannedSession(value)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (a *API) GetWorkouts(w http.ResponseWriter, r *http.Request, id AthleteID, params GetWorkoutsParams) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	from, to := time.Now().UTC().AddDate(0, 0, -730).Format("2006-01-02"), time.Now().UTC().Format("2006-01-02")
	if params.From != nil {
		from = params.From.String()
	}
	if params.To != nil {
		to = params.To.String()
	}
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}
	value, err := a.repo.StrengthWorkouts(r.Context(), id.String(), fitnessrepo.StrengthQuery{From: from, To: to, IncludeEntries: true, Limit: limit, Cursor: valueOrString(params.Cursor)})
	if err != nil {
		writeRepoError(w, err)
		return
	}
	items := make([]Workout, 0, len(value.Items))
	for _, item := range value.Items {
		converted, err := apiWorkout(item)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		items = append(items, converted)
	}
	writeJSON(w, http.StatusOK, WorkoutPage{Items: items, NextCursor: value.NextCursor})
}

func (a *API) GetWorkoutById(w http.ResponseWriter, r *http.Request, id AthleteID, workoutID WorkoutID) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	value, err := a.repo.Workout(r.Context(), id.String(), workoutID.String())
	if err != nil {
		writeRepoError(w, err)
		return
	}
	converted, err := apiWorkout(value)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (a *API) DeleteWorkout(w http.ResponseWriter, r *http.Request, id AthleteID, workoutID WorkoutID) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	if err := a.repo.DeleteWorkoutAggregate(r.Context(), id.String(), workoutID.String()); err != nil {
		writeRepoError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) SaveWorkout(w http.ResponseWriter, r *http.Request, id AthleteID, workoutID WorkoutID) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	body, ok := decodeWorkoutBody(w, r)
	if !ok {
		return
	}
	if body.WorkoutId.String() != workoutID.String() {
		writeError(w, http.StatusBadRequest, "workout_id must match path")
		return
	}
	if body.Status != InProgress {
		writeError(w, http.StatusBadRequest, "workout status must be in_progress")
		return
	}
	value, err := a.repo.UpsertWorkoutAggregate(r.Context(), id.String(), fromAPIWorkout(body))
	if err != nil {
		writeRepoError(w, err)
		return
	}
	converted, err := apiWorkout(value.Workout)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (a *API) CompleteWorkout(w http.ResponseWriter, r *http.Request, id AthleteID, workoutID WorkoutID) {
	if !a.authorizeV2(w, r, id.String()) {
		return
	}
	body, ok := decodeWorkoutBody(w, r)
	if !ok {
		return
	}
	if body.WorkoutId.String() != workoutID.String() {
		writeError(w, http.StatusBadRequest, "workout_id must match path")
		return
	}
	if body.Status != Completed {
		writeError(w, http.StatusBadRequest, "workout status must be completed")
		return
	}
	value, err := a.repo.CompleteWorkout(r.Context(), id.String(), fromAPIWorkout(body))
	if err != nil {
		writeRepoError(w, err)
		return
	}
	converted, err := apiWorkout(value.Workout)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (a *API) PullSyncV2(w http.ResponseWriter, r *http.Request, params PullSyncV2Params) {
	if !a.authorizeV2(w, r, params.AthleteId.String()) {
		return
	}
	after := int64(0)
	if params.After != nil {
		after = *params.After
	}
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}
	value, err := a.repo.PullChanges(r.Context(), params.AthleteId.String(), after, limit)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	changes := make([]SyncChangeV2, 0, len(value.Changes))
	for _, change := range value.Changes {
		kind := SyncChangeV2Kind(change.Kind)
		op := SyncChangeV2Operation(change.Operation)
		entityID, err := uuid.Parse(change.EntityID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "invalid sync entity ID")
			return
		}
		var entity any
		if change.Operation != "delete" {
			switch change.Kind {
			case "training_plan":
				raw, ok := change.Entity.(fitnessrepo.TrainingPlan)
				if !ok {
					writeError(w, http.StatusInternalServerError, "invalid sync plan")
					return
				}
				entity, err = apiPlan(fitnessrepo.PlanResult{Plan: &raw, Sessions: raw.Sessions})
			case "workout":
				raw, ok := change.Entity.(fitnessrepo.StrengthWorkout)
				if !ok {
					writeError(w, http.StatusInternalServerError, "invalid sync workout")
					return
				}
				entity, err = apiWorkout(raw)
			default:
				writeError(w, http.StatusInternalServerError, "invalid sync entity kind")
				return
			}
			if err != nil {
				writeRepoError(w, err)
				return
			}
		}
		changes = append(changes, SyncChangeV2{SyncSeq: change.SyncSeq, Kind: kind, Operation: op, EntityId: openapi_types.UUID(entityID), Entity: entity})
	}
	writeJSON(w, http.StatusOK, SyncPageV2{Changes: changes, NextAfter: value.NextAfter, HasMore: value.HasMore})
}

func (a *API) authorizeV2(w http.ResponseWriter, r *http.Request, athleteID string) bool {
	if err := a.auth.Authorize(r, athleteID); err != nil {
		writeAuthorizationError(w, err)
		return false
	}
	return true
}

func decodeWorkoutBody(w http.ResponseWriter, r *http.Request) (Workout, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workout JSON")
		return Workout{}, false
	}
	if len(raw) > maxRequestBody {
		writeError(w, http.StatusRequestEntityTooLarge, "workout request body is too large")
		return Workout{}, false
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var body Workout
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workout JSON")
		return Workout{}, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid workout JSON")
		return Workout{}, false
	}
	return body, true
}

func writeRepoError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case strings.HasPrefix(message, "invalid_input:"):
		writeError(w, http.StatusBadRequest, message)
	case strings.HasPrefix(message, "not_found:"):
		writeError(w, http.StatusNotFound, message)
	case strings.HasPrefix(message, "conflict:"):
		writeError(w, http.StatusConflict, message)
	default:
		writeError(w, http.StatusInternalServerError, message)
	}
}

func valueOrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func apiPlan(value fitnessrepo.PlanResult) (TrainingPlan, error) {
	if value.Plan == nil {
		return TrainingPlan{}, errors.New("internal_error: plan is missing")
	}
	planID, err := uuid.Parse(value.Plan.ID)
	if err != nil {
		return TrainingPlan{}, errors.New("internal_error: plan ID")
	}
	athleteID, err := uuid.Parse(value.Plan.AthleteID)
	if err != nil {
		return TrainingPlan{}, errors.New("internal_error: plan athlete ID")
	}
	goal := map[string]interface{}{}
	if len(value.Plan.Goal) != 0 && json.Unmarshal(value.Plan.Goal, &goal) != nil {
		return TrainingPlan{}, errors.New("internal_error: plan goal")
	}
	result := TrainingPlan{PlanId: openapi_types.UUID(planID), AthleteId: openapi_types.UUID(athleteID), Title: value.Plan.Title, Goal: goal, Status: TrainingPlanStatus(value.Plan.Status), Source: value.Plan.Source, SyncSeq: value.Plan.SyncSeq, Sessions: []PlannedSession{}}
	result.Description = value.Plan.Description
	result.SourceRef = value.Plan.SourceRef
	if value.Plan.CreatedAt != 0 {
		result.CreatedAt = &value.Plan.CreatedAt
	}
	if value.Plan.StartDate != nil {
		date, e := apiDate(*value.Plan.StartDate)
		if e != nil {
			return TrainingPlan{}, e
		}
		result.StartDate = &date
	}
	if value.Plan.EndDate != nil {
		date, e := apiDate(*value.Plan.EndDate)
		if e != nil {
			return TrainingPlan{}, e
		}
		result.EndDate = &date
	}
	for _, session := range value.Sessions {
		converted, e := apiPlannedSession(session)
		if e != nil {
			return TrainingPlan{}, e
		}
		result.Sessions = append(result.Sessions, converted)
	}
	return result, nil
}

func apiPlannedSession(value fitnessrepo.PlannedSession) (PlannedSession, error) {
	sessionID, err := uuid.Parse(value.ID)
	if err != nil {
		return PlannedSession{}, errors.New("internal_error: session ID")
	}
	planID, err := uuid.Parse(value.PlanID)
	if err != nil {
		return PlannedSession{}, errors.New("internal_error: session plan ID")
	}
	athleteID, err := uuid.Parse(value.AthleteID)
	if err != nil {
		return PlannedSession{}, errors.New("internal_error: session athlete ID")
	}
	result := PlannedSession{SessionId: openapi_types.UUID(sessionID), PlanId: openapi_types.UUID(planID), AthleteId: openapi_types.UUID(athleteID), Title: value.Title, Sport: value.Sport, SessionType: value.SessionType, Priority: value.Priority, Status: PlannedSessionStatus(value.Status), Notes: value.Notes, Description: value.Description, Targets: apiTargets(value.Targets), Steps: []PlannedStep{}}
	if value.CompletedActivityID != nil {
		result.CompletedActivityId = value.CompletedActivityID
	}
	if value.CompletedWorkoutID != nil {
		parsed, e := uuid.Parse(*value.CompletedWorkoutID)
		if e == nil {
			v := openapi_types.UUID(parsed)
			result.CompletedWorkoutId = &v
		}
	}
	for _, step := range value.Steps {
		converted, e := apiStep(step)
		if e != nil {
			return PlannedSession{}, e
		}
		result.Steps = append(result.Steps, converted)
	}
	if value.Groups != nil || value.Steps != nil {
		storedGroups := value.Groups
		if storedGroups == nil {
			storedGroups = []fitnessrepo.PlannedStepGroup{}
		}
		groups := make([]PlannedStepGroup, 0, len(storedGroups))
		for _, group := range storedGroups {
			groupID, e := uuid.Parse(group.ID)
			if e != nil {
				return PlannedSession{}, errors.New("internal_error: planned step group ID")
			}
			converted := PlannedStepGroup{GroupId: openapi_types.UUID(groupID), Mode: PlannedStepGroupMode(group.Mode), Rounds: group.Rounds, StepSequenceNos: append([]int(nil), group.StepSequenceNos...), Notes: group.Notes}
			if group.Label != "" {
				label := group.Label
				converted.Label = &label
			}
			groups = append(groups, converted)
		}
		result.Groups = &groups
	}
	date, err := apiDate(value.Date)
	if err != nil {
		return PlannedSession{}, err
	}
	result.Date = date
	return result, nil
}

func apiStep(value fitnessrepo.PlannedStep) (PlannedStep, error) {
	id, err := uuid.Parse(value.ID)
	if err != nil {
		return PlannedStep{}, errors.New("internal_error: step ID")
	}
	result := PlannedStep{StepId: openapi_types.UUID(id), SequenceNo: value.SequenceNo, StepType: PlannedStepStepType(value.StepType), ExerciseKey: value.ExerciseKey, ExerciseName: value.ExerciseName, Notes: value.Notes}
	if len(value.Prescription) > 0 && json.Unmarshal(value.Prescription, &result.Prescription) != nil {
		return PlannedStep{}, errors.New("internal_error: prescription")
	}
	return result, nil
}

func apiTargets(value fitnessrepo.PlannedTargets) PlannedTargets {
	return PlannedTargets{DurationMinutes: float32Pointer(value.DurationMinutes), DistanceKm: float32Pointer(value.DistanceKM), HeartRateZone: value.HeartRateZone, PaceMinPerKmMin: float32Pointer(value.PaceMinPerKMMin), PaceMinPerKmMax: float32Pointer(value.PaceMinPerKMMax), Sets: value.Sets, Reps: value.Reps, LoadKg: float32Pointer(value.LoadKG)}
}

func apiWorkout(value fitnessrepo.StrengthWorkout) (Workout, error) {
	id, err := uuid.Parse(value.WorkoutID)
	if err != nil {
		return Workout{}, errors.New("internal_error: workout ID")
	}
	athleteID, err := uuid.Parse(value.AthleteID)
	if err != nil {
		return Workout{}, errors.New("internal_error: workout athlete ID")
	}
	result := Workout{WorkoutId: openapi_types.UUID(id), AthleteId: openapi_types.UUID(athleteID), Date: openapi_types.Date{}, Title: value.Title, Source: value.Source, Status: WorkoutStatus(value.Status), LinkedActivityId: value.LinkedActivityID, Notes: value.Notes, DurationMinutes: float32Pointer(value.DurationMinutes), PerceivedEffort: float32Pointer(value.PerceivedEffort), Entries: []WorkoutEntry{}}
	if result.Status == "" {
		result.Status = Completed
	}
	date, err := apiDate(value.Date)
	if err != nil {
		return Workout{}, err
	}
	result.Date = date
	if value.WorkoutType != "" {
		result.WorkoutType = &value.WorkoutType
	}
	if value.PlannedSessionID != nil {
		parsed, e := uuid.Parse(*value.PlannedSessionID)
		if e == nil {
			v := openapi_types.UUID(parsed)
			result.PlannedSessionId = &v
		}
	}
	if value.StartedAt != nil {
		parsed, e := time.Parse(time.RFC3339, *value.StartedAt)
		if e == nil {
			result.StartedAt = &parsed
		}
	}
	if value.CompletedAt != nil {
		parsed, e := time.Parse(time.RFC3339, *value.CompletedAt)
		if e == nil {
			result.CompletedAt = &parsed
		}
	}
	if value.SyncSeq != 0 {
		result.SyncSeq = &value.SyncSeq
	}
	for _, entry := range value.Entries {
		converted, e := apiEntry(entry)
		if e != nil {
			return Workout{}, e
		}
		result.Entries = append(result.Entries, converted)
	}
	if value.PlannedSession != nil {
		converted, e := apiPlannedSession(*value.PlannedSession)
		if e != nil {
			return Workout{}, e
		}
		result.PlannedSession = &converted
	}
	if value.Comparison != nil {
		raw, _ := json.Marshal(value.Comparison)
		var comparison map[string]interface{}
		_ = json.Unmarshal(raw, &comparison)
		result.Comparison = &comparison
	}
	return result, nil
}

func apiEntry(value fitnessrepo.StrengthEntry) (WorkoutEntry, error) {
	id, err := uuid.Parse(value.EntryID)
	if err != nil {
		return WorkoutEntry{}, errors.New("internal_error: entry ID")
	}
	result := WorkoutEntry{EntryId: openapi_types.UUID(id), SequenceNo: value.SequenceNo, EntryType: WorkoutEntryEntryType(value.EntryType), ExerciseKey: value.ExerciseKey, ExerciseName: value.ExerciseName, UserLabel: value.UserLabel, SetNumber: value.SetNumber, Reps: float32Pointer(value.Reps), DurationSeconds: float32Pointer(value.DurationSeconds), LoadKg: float32Pointer(value.LoadKG), LoadDescription: value.LoadDescription, ToFailure: value.ToFailure, Rir: float32Pointer(value.RIR), RirMin: float32Pointer(value.RIRMin), RirMax: float32Pointer(value.RIRMax), Laterality: enumLaterality(value.Laterality), LoadBasis: enumLoadBasis(value.LoadBasis), LoadPerLimbKg: float32Pointer(value.LoadPerLimbKG), TotalExternalLoadKg: float32Pointer(value.TotalExternalLoadKG), AddedLoadKg: float32Pointer(value.AddedLoadKG), RepsPerSide: value.RepsPerSide, Notes: value.Notes, Completed: value.Completed}
	if value.PlannedStepID != nil {
		parsed, e := uuid.Parse(*value.PlannedStepID)
		if e == nil {
			v := openapi_types.UUID(parsed)
			result.PlannedStepId = &v
		}
	}
	if value.IsBodyweight != nil {
		result.IsBodyweight = value.IsBodyweight
	}
	if value.IsWarmup {
		valueCopy := true
		result.IsWarmup = &valueCopy
	}
	if len(value.MuscleGroups) > 0 {
		groups := append([]string(nil), value.MuscleGroups...)
		result.MuscleGroups = &groups
	}
	if len(value.DirectMuscleGroups) > 0 {
		groups := append([]string(nil), value.DirectMuscleGroups...)
		result.DirectMuscleGroups = &groups
	}
	if len(value.IndirectMuscleGroups) > 0 {
		groups := append([]string(nil), value.IndirectMuscleGroups...)
		result.IndirectMuscleGroups = &groups
	}
	return result, nil
}

func enumLaterality(value *string) *WorkoutEntryLaterality {
	if value == nil {
		return nil
	}
	parsed := WorkoutEntryLaterality(*value)
	return &parsed
}
func enumLoadBasis(value *string) *WorkoutEntryLoadBasis {
	if value == nil {
		return nil
	}
	parsed := WorkoutEntryLoadBasis(*value)
	return &parsed
}

func fromAPIWorkout(value Workout) fitnessrepo.WorkoutWrite {
	entries := make([]fitnessrepo.StrengthEntry, 0, len(value.Entries))
	for _, item := range value.Entries {
		var laterality *string
		if item.Laterality != nil {
			value := string(*item.Laterality)
			laterality = &value
		}
		var loadBasis *string
		if item.LoadBasis != nil {
			value := string(*item.LoadBasis)
			loadBasis = &value
		}
		entry := fitnessrepo.StrengthEntry{EntryID: item.EntryId.String(), SequenceNo: item.SequenceNo, EntryType: string(item.EntryType), ExerciseKey: item.ExerciseKey, ExerciseName: item.ExerciseName, UserLabel: item.UserLabel, SetNumber: item.SetNumber, Reps: float64Pointer(item.Reps), DurationSeconds: float64Pointer(item.DurationSeconds), LoadKG: float64Pointer(item.LoadKg), LoadDescription: item.LoadDescription, IsBodyweight: item.IsBodyweight, ToFailure: item.ToFailure, RIR: float64Pointer(item.Rir), RIRMin: float64Pointer(item.RirMin), RIRMax: float64Pointer(item.RirMax), Laterality: laterality, RepsPerSide: item.RepsPerSide, LoadBasis: loadBasis, LoadPerLimbKG: float64Pointer(item.LoadPerLimbKg), TotalExternalLoadKG: float64Pointer(item.TotalExternalLoadKg), AddedLoadKG: float64Pointer(item.AddedLoadKg), Notes: item.Notes, Completed: item.Completed}
		if item.PlannedStepId != nil {
			v := item.PlannedStepId.String()
			entry.PlannedStepID = &v
		}
		if item.MuscleGroups != nil {
			entry.MuscleGroups = append([]string(nil), (*item.MuscleGroups)...)
		}
		if item.DirectMuscleGroups != nil {
			entry.DirectMuscleGroups = append([]string(nil), (*item.DirectMuscleGroups)...)
		}
		if item.IndirectMuscleGroups != nil {
			entry.IndirectMuscleGroups = append([]string(nil), (*item.IndirectMuscleGroups)...)
		}
		if item.IsWarmup != nil {
			entry.IsWarmup = *item.IsWarmup
		}
		entries = append(entries, entry)
	}
	var workoutType string
	if value.WorkoutType != nil {
		workoutType = *value.WorkoutType
	}
	var planned *string
	if value.PlannedSessionId != nil {
		v := value.PlannedSessionId.String()
		planned = &v
	}
	return fitnessrepo.WorkoutWrite{ID: value.WorkoutId.String(), ClientUpdatedAt: time.Now().UnixMilli(), Date: value.Date.String(), Title: value.Title, WorkoutType: workoutType, Source: value.Source, PlannedSessionID: planned, Status: string(value.Status), StartedAt: value.StartedAt, CompletedAt: value.CompletedAt, DurationMinutes: float64Pointer(value.DurationMinutes), PerceivedEffort: float64Pointer(value.PerceivedEffort), Notes: value.Notes, LinkedActivityID: value.LinkedActivityId, Entries: entries}
}

func apiDate(value string) (openapi_types.Date, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return openapi_types.Date{}, errors.New("internal_error: stored date")
	}
	return openapi_types.Date{Time: parsed}, nil
}
func float32Pointer(value *float64) *float32 {
	if value == nil {
		return nil
	}
	converted := float32(*value)
	return &converted
}
func float64Pointer(value *float32) *float64 {
	if value == nil {
		return nil
	}
	converted := float64(*value)
	return &converted
}

var _ ServerInterface = (*API)(nil)
