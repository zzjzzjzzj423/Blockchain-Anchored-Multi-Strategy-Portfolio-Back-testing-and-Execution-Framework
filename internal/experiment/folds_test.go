package experiment

import (
	"testing"
	"time"
)

func TestWalkForwardFoldBoundaries(t *testing.T) {
	folds := WalkForwardFolds()
	if err := ValidateFolds(folds); err != nil {
		t.Fatal(err)
	}
	wantStarts := []string{"2023-01-01", "2023-07-01", "2024-01-01", "2024-07-01"}
	wantEnds := []string{"2023-06-30", "2023-12-31", "2024-06-30", "2024-12-31"}
	for index, fold := range folds {
		if fold.ValidationStart.Format("2006-01-02") != wantStarts[index] || fold.ValidationEnd.Format("2006-01-02") != wantEnds[index] {
			t.Fatalf("fold %d = %#v", index+1, fold)
		}
	}
}

func TestValidateFoldsRejectsGap(t *testing.T) {
	folds := WalkForwardFolds()
	folds[1].ValidationStart = folds[1].ValidationStart.AddDate(0, 0, 1)
	if err := ValidateFolds(folds); err == nil {
		t.Fatal("expected boundary error")
	}
	if err := ValidateFolds(folds[:3]); err == nil {
		t.Fatal("expected count error")
	}
	folds = WalkForwardFolds()
	folds[0].TrainStart = time.Date(2021, 1, 1, 0, 0, 0, 0, time.Local)
	if err := ValidateFolds(folds); err == nil {
		t.Fatal("expected timezone error")
	}
}
