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
func strengthSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "exercise_keys": array(str(128), 0, 30), "muscle_groups": array(str(128), 0, 30), "include_entries": map[string]any{"type": "boolean"}, "order": orderProp(), "limit": integer(1, 100), "cursor": cursorProp()}, "from", "to")
}

func entrySchema() map[string]any {
	return obj(map[string]any{"entry_id": str(64), "sequence_no": integer(1, 10000), "entry_type": enum("set", "rest", "note"), "exercise_key": nullable(str(128)), "exercise_name": nullable(str(256)), "user_label": nullable(str(256)), "set_number": nullable(integer(1, 10000)), "reps": nullable(number(0, 1000)), "duration_seconds": nullable(number(0, 86400)), "load_kg": nullable(number(0, 2000)), "load_description": nullable(str(256)), "is_bodyweight": map[string]any{"type": "boolean"}, "to_failure": map[string]any{"type": "boolean"}, "rir": nullable(number(0, 10)), "rir_min": nullable(number(0, 10)), "rir_max": nullable(number(0, 10)), "laterality": nullable(str(64)), "muscle_groups": array(str(128), 0, 10), "notes": nullable(str(4000)), "is_warmup": map[string]any{"type": "boolean"}}, "entry_id", "sequence_no", "entry_type")
}
func upsertWorkoutSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "workout_id": str(64), "client_updated_at": integer(0, 9223372036854775807), "date": dateSchema(), "title": str(512), "workout_type": enum("strength", "circuit", "mobility", "cardio", "run", "other"), "source": str(128), "started_at": nullable(str(64)), "duration_minutes": nullable(number(0, 1440)), "perceived_effort": nullable(number(1, 10)), "notes": nullable(str(4000)), "linked_activity_id": nullable(str(128)), "entries": array(entrySchema(), 0, 200)}, "workout_id", "client_updated_at", "date", "title", "workout_type", "source")
}
func deleteWorkoutSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "workout_id": str(64), "client_updated_at": integer(0, 9223372036854775807)}, "workout_id", "client_updated_at")
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
	return obj(map[string]any{"target_athlete_id": targetProp(), "checkin_id": str(64), "client_updated_at": integer(0, 9223372036854775807), "recorded_at": str(64), "energy": nullable(integer(1, 5)), "fatigue": nullable(integer(1, 5)), "motivation": nullable(integer(1, 5)), "sleep_quality": nullable(integer(1, 5)), "stress": nullable(integer(1, 5)), "soreness": array(painSchema(), 0, 20), "pain": array(painSchema(), 0, 20), "illness": illnessSchema(), "notes": nullable(str(4000))}, "checkin_id", "client_updated_at", "recorded_at")
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
	return obj(map[string]any{"target_athlete_id": targetProp(), "from": dateSchema(), "to": dateSchema(), "include_completed": map[string]any{"type": "boolean"}}, "from", "to")
}
func targetsSchema() map[string]any {
	return obj(map[string]any{"duration_minutes": nullable(number(0, 1440)), "distance_km": nullable(number(0, 10000)), "heart_rate_zone": nullable(str(32)), "pace_min_per_km_min": nullable(number(0, 1000)), "pace_min_per_km_max": nullable(number(0, 1000)), "sets": nullable(integer(0, 10000)), "reps": nullable(integer(0, 100000)), "load_kg": nullable(number(0, 2000))})
}
func plannedSessionSchema() map[string]any {
	return obj(map[string]any{"target_athlete_id": targetProp(), "plan_id": str(64), "session_id": str(64), "client_updated_at": integer(0, 9223372036854775807), "date": dateSchema(), "sport": enum("running", "walking", "cycling", "swimming", "strength", "cardio", "mobility", "hiking", "multisport", "other"), "session_type": str(64), "priority": enum("optional", "normal", "key"), "targets": targetsSchema(), "notes": nullable(str(4000))}, "plan_id", "session_id", "client_updated_at", "date", "sport", "session_type", "priority")
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
