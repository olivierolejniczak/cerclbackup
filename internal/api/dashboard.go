package api

import (
	"time"

	"github.com/cerclbackup/cerclbackup/internal/storage"
)

// DashboardStatus is "green"/"yellow"/"red" summarizing overall health.
type DashboardStatus string

const (
	StatusGreen  DashboardStatus = "green"
	StatusYellow DashboardStatus = "yellow"
	StatusRed    DashboardStatus = "red"
)

// DashboardResult combines Doctor, BuddyStatus and Storage signals into a
// single at-a-glance health summary for the GUI's dashboard view.
type DashboardResult struct {
	Status       DashboardStatus
	Doctor       *DoctorResult
	Buddies      []BuddyStatusEntry
	BuddiesTotal int
	BuddiesUp    int
	Storage      *StorageStats
}

// Dashboard runs Doctor, BuddyStatus and Storage and derives an overall
// green/yellow/red status:
//   - red: Doctor reports a failing check, or zero buddies are reachable
//     while at least one is registered
//   - yellow: no buddies registered yet, or last backup is aging (Doctor
//     still passes but with a warning-shaped check)
//   - green: everything is fine
func Dashboard(password, storeDir string) (*DashboardResult, error) {
	if storeDir == "" {
		storeDir = storage.DefaultStorePath()
	}

	doctor, err := Doctor(DoctorParams{Password: password, StoreDir: storeDir, CheckBuddies: false, MaxAge: 25 * time.Hour})
	if err != nil {
		return nil, err
	}

	buddies, err := BuddyStatus(password, 5*time.Second)
	if err != nil {
		return nil, err
	}
	up := 0
	for _, b := range buddies {
		if b.Online {
			up++
		}
	}

	stats, err := Storage(password, storeDir)
	if err != nil {
		return nil, err
	}

	status := StatusGreen
	if !doctor.AllOK {
		status = StatusRed
	} else if len(buddies) == 0 {
		status = StatusYellow
	} else if up == 0 {
		status = StatusRed
	} else if up < len(buddies) {
		status = StatusYellow
	}

	return &DashboardResult{
		Status:       status,
		Doctor:       doctor,
		Buddies:      buddies,
		BuddiesTotal: len(buddies),
		BuddiesUp:    up,
		Storage:      stats,
	}, nil
}
