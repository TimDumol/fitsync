import unittest

from garmin_sync import _without_activity_location, summarize_activity
from garmin_sleep_probe import summarize_sleep


class SummarizeSleepTests(unittest.TestCase):
    def test_extracts_sleep_and_recovery_fields(self) -> None:
        sleep = {
            "dailySleepDTO": {
                "calendarDate": "2026-08-18",
                "sleepTimeSeconds": 27_000,
                "napTimeSeconds": 2_700,
                "deepSleepSeconds": 4_500,
                "lightSleepSeconds": 15_300,
                "remSleepSeconds": 7_200,
                "awakeSleepSeconds": 1_800,
                "averageSpO2Value": 96,
                "avgSleepStress": 13,
                "sleepScores": {
                    "overall": {"value": 82, "qualifierKey": "GOOD"}
                },
            }
        }
        hrv = {"hrvSummary": {"lastNightAvg": 48, "weeklyAvg": 45, "status": "BALANCED"}}
        rhr = {
            "allMetrics": {
                "metricsMap": {"WELLNESS_RESTING_HEART_RATE": [{"value": 51}]}
            }
        }

        result = summarize_sleep("2026-08-18", sleep, hrv, rhr)

        self.assertEqual(result["total_sleep_hours"], 7.5)
        self.assertEqual(result["nap_hours"], 0.75)
        self.assertEqual(result["total_sleep_including_naps_hours"], 8.25)
        self.assertEqual(result["deep_sleep_hours"], 1.25)
        self.assertEqual(result["sleep_score"], 82)
        self.assertEqual(result["overnight_hrv"], 48)
        self.assertEqual(result["resting_heart_rate"], 51)

    def test_summarizes_activity_in_analysis_units(self) -> None:
        result = summarize_activity(
            {
                "activityId": 123,
                "activityName": "Morning Run",
                "activityType": {"typeKey": "running"},
                "startTimeLocal": "2026-08-19 08:00:00",
                "distance": 5000.0,
                "duration": 1800.0,
                "movingDuration": 1750.0,
                "averageSpeed": 2.857,
                "averageHR": 155,
                "aerobicTrainingEffect": 3.2,
            }
        )

        self.assertEqual(result["activity_type"], "running")
        self.assertEqual(result["distance_km"], 5.0)
        self.assertEqual(result["duration_minutes"], 30.0)
        self.assertEqual(result["average_pace_min_per_km"], 5.83)
        self.assertEqual(result["average_speed_kmh"], 10.29)
        self.assertEqual(result["average_heart_rate"], 155.0)
        self.assertEqual(result["aerobic_training_effect"], 3.2)

    def test_removes_indexed_coordinates_from_activity_details(self) -> None:
        result = _without_activity_location(
            {
                "metric_time_series": {
                    "metricsCount": 3,
                    "metricDescriptors": [
                        {"metricsIndex": 0, "key": "sumDuration"},
                        {"metricsIndex": 1, "key": "directLatitude"},
                        {"metricsIndex": 2, "key": "directLongitude"},
                    ],
                    "activityDetailMetrics": [{"metrics": [12.0, 41.1, 2.1]}],
                },
                "startLatitude": 41.1,
            }
        )

        series = result["metric_time_series"]
        self.assertEqual(series["metricsCount"], 1)
        self.assertEqual(series["metricDescriptors"], [{"metricsIndex": 0, "key": "sumDuration"}])
        self.assertEqual(series["activityDetailMetrics"], [{"metrics": [12.0]}])
        self.assertNotIn("startLatitude", result)


if __name__ == "__main__":
    unittest.main()
