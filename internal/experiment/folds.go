package experiment

import (
	"fmt"
	"time"
)

type Fold struct {
	ID              int
	TrainStart      time.Time
	TrainEnd        time.Time
	ValidationStart time.Time
	ValidationEnd   time.Time
}

func WalkForwardFolds() []Fold {
	date := func(value string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02", value, time.UTC)
		if err != nil {
			panic(err)
		}
		return parsed
	}
	return []Fold{
		{ID: 1, TrainStart: date("2021-01-01"), TrainEnd: date("2022-12-31"), ValidationStart: date("2023-01-01"), ValidationEnd: date("2023-06-30")},
		{ID: 2, TrainStart: date("2021-01-01"), TrainEnd: date("2023-06-30"), ValidationStart: date("2023-07-01"), ValidationEnd: date("2023-12-31")},
		{ID: 3, TrainStart: date("2021-01-01"), TrainEnd: date("2023-12-31"), ValidationStart: date("2024-01-01"), ValidationEnd: date("2024-06-30")},
		{ID: 4, TrainStart: date("2021-01-01"), TrainEnd: date("2024-06-30"), ValidationStart: date("2024-07-01"), ValidationEnd: date("2024-12-31")},
	}
}

func ValidateFolds(folds []Fold) error {
	if len(folds) != 4 {
		return fmt.Errorf("walk-forward protocol requires four folds")
	}
	for index, fold := range folds {
		if fold.ID != index+1 || fold.TrainStart.Location() != time.UTC || fold.ValidationStart.Location() != time.UTC {
			return fmt.Errorf("fold %d has invalid identity or timezone", index+1)
		}
		if fold.TrainEnd.Before(fold.TrainStart) || !fold.ValidationStart.Equal(fold.TrainEnd.AddDate(0, 0, 1)) || fold.ValidationEnd.Before(fold.ValidationStart) {
			return fmt.Errorf("fold %d has invalid boundaries", fold.ID)
		}
		if index > 0 && !fold.ValidationStart.Equal(folds[index-1].ValidationEnd.AddDate(0, 0, 1)) {
			return fmt.Errorf("validation folds are not contiguous")
		}
	}
	return nil
}
