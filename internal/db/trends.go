package db

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Trend struct {
	Date               string             `json:"date"`
	TotalCost          float64            `json:"total_cost"`
	ResourceCount      int                `json:"resource_count"`
	BreakdownByService map[string]float64 `json:"breakdown_by_service"`
}

func ListTrends(ctx context.Context, pool *pgxpool.Pool, days int) ([]Trend, error) {
	snapshots, err := ListSnapshots(ctx, pool, days)
	if err != nil {
		return nil, fmt.Errorf("loading snapshots: %w", err)
	}

	type dayServiceKey struct {
		date    string
		service string
	}

	latest := make(map[dayServiceKey]Snapshot)
	for _, s := range snapshots {
		date := s.TakenAt.Format(time.DateOnly)
		k := dayServiceKey{date: date, service: s.Service}
		existing, ok := latest[k]
		if !ok || s.TakenAt.After(existing.TakenAt) {
			latest[k] = s
		}
	}

	byDate := make(map[string]*Trend)
	for k, s := range latest {
		t, ok := byDate[k.date]
		if !ok {
			t = &Trend{
				Date:               k.date,
				BreakdownByService: make(map[string]float64),
			}
			byDate[k.date] = t
		}
		t.TotalCost += s.TotalMonthlyCost
		t.ResourceCount += s.ResourceCount
		t.BreakdownByService[s.Service] += s.TotalMonthlyCost
	}

	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	trends := make([]Trend, 0, len(dates))
	for _, date := range dates {
		trends = append(trends, *byDate[date])
	}
	return trends, nil
}
