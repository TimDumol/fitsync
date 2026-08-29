package fitnessdata

import "encoding/json"

type TrainingSnapshot struct {
	Status       *string  `json:"status,omitempty"`
	VO2Max       *float64 `json:"vo2_max,omitempty"`
	TrainingLoad *float64 `json:"training_load,omitempty"`
}

func DecodeTrainingSnapshot(_ string, payload json.RawMessage) (DecoderResult[TrainingSnapshot], error) {
	object, err := decodeObject(payload)
	if err != nil {
		return DecoderResult[TrainingSnapshot]{}, err
	}
	value := TrainingSnapshot{Status: text(object, "trainingStatus", "training_status", "status"), VO2Max: firstNumber(object, "vo2Max", "vo2_max"), TrainingLoad: firstNumber(object, "trainingLoad", "training_load")}
	return DecoderResult[TrainingSnapshot]{Value: value, Completeness: Availability{"training": value.Status != nil || value.VO2Max != nil || value.TrainingLoad != nil}}, nil
}
