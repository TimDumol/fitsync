package mcpserver

import "github.com/modelcontextprotocol/go-sdk/mcp"

func obj(properties map[string]any, required ...string) map[string]any {
	result := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}
func str(max int) map[string]any {
	result := map[string]any{"type": "string"}
	if max > 0 {
		result["maxLength"] = max
	}
	return result
}
func exerciseKeyProp() map[string]any {
	return map[string]any{
		"type":        "string",
		"maxLength":   128,
		"pattern":     "^[a-z0-9]+(?:_[a-z0-9]+)*$",
		"description": "Canonical key previously returned by FitSync. Omit when uncertain; never invent one.",
	}
}
func enum(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }
func integer(min, max int64) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "maximum": max}
}
func number(min, max float64) map[string]any {
	return map[string]any{"type": "number", "minimum": min, "maximum": max}
}
func array(items map[string]any, min, max int) map[string]any {
	result := map[string]any{"type": "array", "items": items}
	if min > 0 {
		result["minItems"] = min
	}
	if max > 0 {
		result["maxItems"] = max
	}
	return result
}
func nullable(schema map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}
func dateSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"}
}
func targetProp() map[string]any {
	return map[string]any{"type": "string", "format": "uuid", "maxLength": 36}
}
func uuidv7Prop() map[string]any {
	return map[string]any{
		"type":        "string",
		"format":      "uuid",
		"maxLength":   36,
		"pattern":     "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$",
		"description": "Optional UUIDv7. The server generates one when omitted.",
	}
}
func idempotencyKeyProp() map[string]any {
	return map[string]any{
		"type":        "string",
		"maxLength":   256,
		"description": "Optional caller-supplied stable key used to make retries address the same record.",
	}
}
func targetOnlySchema() map[string]any { return obj(map[string]any{"target_athlete_id": targetProp()}) }
func orderProp() map[string]any        { return enum("asc", "desc") }
func cursorProp() map[string]any       { return map[string]any{"type": "string", "maxLength": 512} }

func trainingContextSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "as_of": dateSchema(), "lookback_days": integer(7, 90), "include": array(enum("recovery", "training_load", "recent_activities", "strength", "plan", "checkins", "goals"), 1, 7)})
}
func dailyHealthSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "metrics": array(enum("sleep", "hrv", "heart_rate", "stress", "body_battery", "spo2", "daily_activity"), 1, 7), "order": orderProp(), "limit": integer(1, 100), "cursor": cursorProp()}, "from", "to")
}
func activitySchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "categories": array(str(64), 0, 20), "source_types": array(str(128), 0, 20), "detail": enum("compact", "summary", "full"), "min_duration_minutes": nullable(number(0, 1440)), "min_distance_km": nullable(number(0, 10000)), "min_training_load": nullable(number(0, 100000)), "order": orderProp(), "limit": integer(1, 100), "cursor": cursorProp()}, "from", "to")
}
func activityDetailSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "activity_id": str(128), "include": array(enum("summary", "splits", "zones", "metrics"), 1, 4), "metric_resolution_seconds": integer(5, 300), "max_metric_points": integer(10, 1000)}, "activity_id")
}
func activityIntervalsSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "activity_id": str(128), "include_laps": map[string]any{"type": "boolean"}, "include_recoveries": map[string]any{"type": "boolean"}, "max_segments": integer(1, 100)}, "activity_id")
}
func timelineSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "include_unplanned": map[string]any{"type": "boolean"}, "include_entries": map[string]any{"type": "boolean"}, "order": orderProp(), "limit": integer(1, 100), "cursor": cursorProp()}, "from", "to")
}
func readinessSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "as_of": str(64), "session_date": dateSchema(), "proposed_session_type": str(64), "proposed_duration_minutes": integer(1, 1440), "proposed_intensity": enum("easy", "moderate", "hard", "maximal")}, "session_date", "proposed_session_type", "proposed_duration_minutes", "proposed_intensity")
}
func strengthSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "exercise_keys": array(exerciseKeyProp(), 0, 30), "muscle_groups": array(str(128), 0, 30), "include_entries": map[string]any{"type": "boolean"}, "order": orderProp(), "limit": integer(1, 100), "cursor": cursorProp()}, "from", "to")
}

func exerciseProgressSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "exercise_key": exerciseKeyProp(), "from": dateSchema(), "to": dateSchema(), "include_warmups": map[string]any{"type": "boolean"}, "limit": integer(1, 100), "cursor": cursorProp()}, "exercise_key", "from", "to")
}

func resolveExercisesSchema() map[string]any {
	item := obj(map[string]any{"ref": str(128), "name": str(256)}, "ref", "name")
	return obj(map[string]any{"target_athlete_id": targetProp(), "exercises": array(item, 1, 100), "max_candidates": integer(1, 10)}, "exercises")
}

func entrySchema() map[string]any {
	result := obj(map[string]any{"entry_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "sequence_no": integer(1, 10000), "entry_type": enum("set", "rest", "note"), "exercise_key": nullable(exerciseKeyProp()), "exercise_name": nullable(str(256)), "user_label": nullable(str(256)), "set_number": nullable(integer(1, 10000)), "reps": nullable(number(0, 1000)), "duration_seconds": nullable(number(0, 86400)), "load_kg": nullable(number(0, 2000)), "load_description": nullable(str(256)), "is_bodyweight": map[string]any{"type": "boolean"}, "to_failure": map[string]any{"type": "boolean"}, "rir": nullable(number(0, 10)), "rir_min": nullable(number(0, 10)), "rir_max": nullable(number(0, 10)), "laterality": nullable(enum("bilateral", "unilateral", "left", "right", "alternating", "unknown")), "reps_per_side": map[string]any{"type": "boolean"}, "load_basis": nullable(enum("total_external", "per_limb", "bodyweight_added", "assistance", "unknown")), "load_per_limb_kg": nullable(number(0, 2000)), "total_external_load_kg": nullable(number(0, 4000)), "added_load_kg": nullable(number(0, 2000)), "muscle_groups": array(str(128), 0, 10), "direct_muscle_groups": array(str(128), 0, 10), "indirect_muscle_groups": array(str(128), 0, 10), "notes": nullable(str(4000)), "completed": map[string]any{"type": "boolean"}, "is_warmup": map[string]any{"type": "boolean"}}, "sequence_no", "entry_type")
	result["properties"].(map[string]any)["exercise_name"].(map[string]any)["description"] = "Free-form exercise label or coaching description; preserved as supplied."
	return result
}
func upsertWorkoutSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "workout_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "client_updated_at": integer(0, 9223372036854775807), "date": dateSchema(), "title": str(512), "workout_type": enum("strength", "circuit", "mobility", "cardio", "run", "other"), "source": str(128), "started_at": nullable(str(64)), "duration_minutes": nullable(number(0, 1440)), "perceived_effort": nullable(number(1, 10)), "notes": nullable(str(4000)), "linked_activity_id": nullable(str(128)), "entries": array(entrySchema(), 0, 200)}, "client_updated_at", "date", "title", "workout_type", "source")
}
func deleteWorkoutSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "workout_id": str(64), "client_updated_at": integer(0, 9223372036854775807)}, "workout_id", "client_updated_at")
}
func patchWorkoutSchema() map[string]any {
	operation := obj(map[string]any{"op": enum("set_linked_activity", "set_entry_rir", "set_entry_exercise_key", "set_entry_load_semantics", "set_entry_muscle_roles", "append_note"), "activity_id": str(128), "entry_id": str(128), "rir": number(0, 10), "exercise_key": exerciseKeyProp(), "laterality": enum("bilateral", "unilateral", "left", "right", "alternating", "unknown"), "reps_per_side": map[string]any{"type": "boolean"}, "load_basis": enum("total_external", "per_limb", "bodyweight_added", "assistance", "unknown"), "load_per_limb_kg": number(0, 2000), "total_external_load_kg": number(0, 4000), "added_load_kg": number(0, 2000), "direct_muscle_groups": array(str(128), 0, 10), "indirect_muscle_groups": array(str(128), 0, 10), "text": str(4000)}, "op")
	return obj(map[string]any{"target_athlete_id": targetProp(), "workout_id": str(128), "expected_sync_seq": integer(0, 9223372036854775807), "client_updated_at": integer(0, 9223372036854775807), "operations": array(operation, 1, 50)}, "workout_id", "expected_sync_seq", "client_updated_at", "operations")
}
func painSchema() map[string]any {
	return obj(map[string]any{"area": str(128), "severity": integer(1, 5)}, "area", "severity")
}
func illnessSchema() map[string]any {
	return obj(map[string]any{"present": map[string]any{"type": "boolean"}, "symptoms": array(str(128), 0, 20)}, "present")
}
func checkinsSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "order": orderProp(), "limit": integer(1, 100), "cursor": cursorProp()}, "from", "to")
}
func recordCheckinSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "checkin_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "client_updated_at": integer(0, 9223372036854775807), "recorded_at": str(64), "energy": nullable(integer(1, 5)), "fatigue": nullable(integer(1, 5)), "motivation": nullable(integer(1, 5)), "sleep_quality": nullable(integer(1, 5)), "stress": nullable(integer(1, 5)), "soreness": array(painSchema(), 0, 20), "pain": array(painSchema(), 0, 20), "illness": illnessSchema(), "notes": nullable(str(4000))}, "client_updated_at", "recorded_at")
}
func quickReadinessSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "checkin_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "client_updated_at": integer(0, 9223372036854775807), "recorded_at": str(64), "energy": integer(1, 5), "leg_freshness": integer(1, 5), "sleep_quality": integer(1, 5), "pain_present": map[string]any{"type": "boolean"}, "pain": array(painSchema(), 0, 20)}, "client_updated_at", "recorded_at", "energy", "leg_freshness", "sleep_quality", "pain_present")
}

func goalSchema() map[string]any {
	return obj(map[string]any{"name": str(256), "target_value": nullable(number(-1e9, 1e9)), "unit": nullable(str(64)), "target_date": nullable(dateSchema())}, "name")
}
func eventSchema() map[string]any {
	return obj(map[string]any{"name": str(256), "sport": str(64), "date": dateSchema(), "distance_km": nullable(number(0, 10000))}, "name", "sport", "date")
}
func windowSchema() map[string]any {
	return obj(map[string]any{"start": str(16), "end": str(16)}, "start", "end")
}
func zoneSchema() map[string]any {
	return obj(map[string]any{"name": str(64), "min": nullable(number(0, 1e9)), "max": nullable(number(0, 1e9))})
}
func conditionSchema() map[string]any {
	return obj(map[string]any{"label": str(256), "status": enum("active", "managed", "resolved"), "notes": nullable(str(1000))}, "label", "status")
}
func profileSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "client_updated_at": integer(0, 9223372036854775807), "timezone": str(64), "units": enum("metric"), "birth_year": nullable(integer(1900, 2100)), "goals": array(goalSchema(), 0, 10), "target_events": array(eventSchema(), 0, 20), "weekly_availability": map[string]any{"type": "object", "additionalProperties": array(windowSchema(), 0, 5)}, "preferences": obj(map[string]any{"run_days": integer(0, 7), "strength_sessions": integer(0, 7), "preferred_weekdays": array(enum("monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"), 0, 7), "back_to_back_hard_days": map[string]any{"type": "boolean"}}), "heart_rate_zones": array(zoneSchema(), 0, 10), "pace_zones": array(zoneSchema(), 0, 10), "power_zones": array(zoneSchema(), 0, 10), "constraints": map[string]any{"type": "object", "additionalProperties": map[string]any{}}, "conditions_and_injuries": array(conditionSchema(), 0, 20)}, "client_updated_at", "timezone", "units")
}
func planSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "plan_id": str(64), "include_steps": map[string]any{"type": "boolean"}, "include_completed": map[string]any{"type": "boolean"}}, "from", "to")
}
func targetsSchema() map[string]any {
	return obj(map[string]any{"duration_minutes": nullable(number(0, 1440)), "distance_km": nullable(number(0, 10000)), "heart_rate_zone": nullable(str(32)), "pace_min_per_km_min": nullable(number(0, 1000)), "pace_min_per_km_max": nullable(number(0, 1000)), "sets": nullable(integer(0, 10000)), "reps": nullable(integer(0, 100000)), "load_kg": nullable(number(0, 2000))})
}
func plannedSessionSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "plan_id": uuidv7Prop(), "plan_idempotency_key": idempotencyKeyProp(), "session_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "client_updated_at": integer(0, 9223372036854775807), "date": dateSchema(), "sport": enum("running", "walking", "cycling", "swimming", "strength", "cardio", "mobility", "hiking", "multisport", "other"), "session_type": str(64), "priority": enum("optional", "normal", "key"), "targets": targetsSchema(), "notes": nullable(str(4000))}, "client_updated_at", "date", "sport", "session_type", "priority")
}

func prescriptionSetSchema() map[string]any {
	return obj(map[string]any{
		"reps_min": nullable(integer(0, 1000)), "reps_max": nullable(integer(0, 1000)),
		"load_kg": nullable(number(0, 2000)), "rir": nullable(number(0, 10)), "rpe": nullable(number(1, 10)),
		"duration_seconds": nullable(integer(0, 86400)), "rest_seconds": nullable(integer(0, 86400)), "tempo": nullable(str(32)),
	})
}
func prescriptionSchema() map[string]any {
	return obj(map[string]any{
		"sets": array(prescriptionSetSchema(), 0, 20), "rounds": nullable(integer(0, 1000)),
		"distance_meters": nullable(number(0, 1000000)), "duration_seconds": nullable(integer(0, 86400)),
		"pace_min_per_km_min": nullable(number(0, 1000)), "pace_min_per_km_max": nullable(number(0, 1000)),
		"heart_rate_zone": nullable(str(64)),
	})
}
func plannedStepSchema() map[string]any {
	result := obj(map[string]any{
		"step_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "planned_session_id": nullable(str(64)), "athlete_id": nullable(str(64)),
		"sequence_no": integer(1, 50), "step_type": enum("warmup", "exercise", "interval", "rest", "cooldown", "note"),
		"exercise_key": nullable(exerciseKeyProp()), "exercise_name": nullable(str(256)), "prescription": prescriptionSchema(), "notes": nullable(str(4000)),
	}, "sequence_no", "step_type", "prescription")
	result["properties"].(map[string]any)["exercise_name"].(map[string]any)["description"] = "Free-form exercise label or coaching description; preserved as supplied."
	return result
}
func plannedStepGroupSchema() map[string]any {
	members := array(integer(1, 50), 2, 10)
	members["uniqueItems"] = true
	result := obj(map[string]any{
		"group_id":          uuidv7Prop(),
		"idempotency_key":   idempotencyKeyProp(),
		"label":             str(128),
		"mode":              enum("rounds"),
		"rounds":            integer(1, 20),
		"step_sequence_nos": members,
		"notes":             nullable(str(4000)),
	}, "mode", "rounds", "step_sequence_nos")
	result["description"] = "Execution structure for a non-nested round group. Use this for supersets and circuits; do not encode execution structure only in notes. A two-member group is a superset, and each member's rest_seconds is recovery after that member."
	return result
}
func planDraftSessionSchema() map[string]any {
	result := obj(map[string]any{
		"session_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "date": dateSchema(), "title": str(512), "description": nullable(str(4000)),
		"sport": str(64), "session_type": str(64), "priority": str(32), "targets": targetsSchema(), "notes": nullable(str(4000)),
		"steps": array(plannedStepSchema(), 0, 50), "groups": array(plannedStepGroupSchema(), 0, 50),
	}, "date", "sport", "session_type", "priority", "steps")
	result["properties"].(map[string]any)["groups"].(map[string]any)["description"] = "Use groups for all execution-critical supersets and circuits; do not encode those relationships only in notes."
	return result
}
func planDraftSchema() map[string]any {
	return obj(map[string]any{
		"target_athlete_id": targetProp(), "plan_id": uuidv7Prop(), "idempotency_key": idempotencyKeyProp(), "title": str(512), "description": nullable(str(4000)),
		"goal": map[string]any{"type": "object", "additionalProperties": true}, "start_date": nullable(dateSchema()), "end_date": nullable(dateSchema()),
		"source_ref": nullable(str(512)), "sessions": array(planDraftSessionSchema(), 0, 120),
	}, "title", "sessions")
}
func publishPlanSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "plan_id": str(64)}, "plan_id")
}
func workoutSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "workout_id": str(64)}, "workout_id")
}
func plannedStatusSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "session_id": str(64), "client_updated_at": integer(0, 9223372036854775807), "status": enum("planned", "completed", "skipped", "cancelled"), "completed_activity_id": nullable(str(128)), "completed_workout_id": nullable(str(64))}, "session_id", "client_updated_at", "status")
}
func analysisSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "compare_with_previous_period": map[string]any{"type": "boolean"}, "group_by": enum("day", "week", "month"), "categories": array(enum("running", "walking", "cycling", "swimming", "strength", "cardio", "mobility", "hiking", "multisport", "other"), 0, 12)}, "from", "to")
}
func legacyUpdateSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "kind": enum("session", "entry", "document"), "id": str(128), "client_updated_at": integer(0, 9223372036854775807), "deleted": map[string]any{"type": "boolean"}, "workout_date": dateSchema(), "title": str(512), "source": str(128), "sequence_no": integer(1, 10000), "entry_type": str(128), "session_id": str(128), "document_key": str(512), "payload": map[string]any{}}, "kind", "id", "client_updated_at")
}
func boolPtr(value bool) *bool { return &value }

func readAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)}
}

func writeAnnotations(destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: boolPtr(destructive), OpenWorldHint: boolPtr(false)}
}
