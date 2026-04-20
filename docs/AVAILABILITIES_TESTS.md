# Tests du Module Availabilities

## Unit Tests

### Test IsProductAvailable

```go
package availabilities

import (
	"context"
	"testing"
	"time"
)

// TestIsProductAvailable_NoAvailabilities tests when no availability is defined
func TestIsProductAvailable_NoAvailabilities(t *testing.T) {
	svc := &AvailabilitiesService{
		availabilitiesRepo: &MockRepository{
			availabilities: []Availability{},
		},
	}
	
	isAvailable, err := svc.IsProductAvailable(context.Background(), "merchant-1", "product-1")
	
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isAvailable {
		t.Fatal("product should be available by default when no availability is defined")
	}
}

// TestIsProductAvailable_WithinTimeRange tests when product is within time range
func TestIsProductAvailable_WithinTimeRange(t *testing.T) {
	// Simule lundi 9h UTC
	monday9am := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	
	schedules := []AvailabilitySchedule{
		{
			DayOfWeek: 2, // Lundi
			StartTime: "08:00:00",
			EndTime:   "11:00:00",
		},
	}
	
	availabilities := []Availability{
		{
			AvailabilityID: "avail-1",
			Schedules:      schedules,
		},
	}
	
	svc := &AvailabilitiesService{
		availabilitiesRepo: &MockRepository{
			availabilities: availabilities,
		},
	}
	
	// Mock time.Now() → lundi 9h
	isAvailable, err := svc.IsProductAvailable(context.Background(), "merchant-1", "product-1")
	
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isAvailable {
		t.Fatal("product should be available between 08:00 and 11:00 on Monday")
	}
}

// TestIsProductAvailable_OutsideTimeRange tests when product is outside time range
func TestIsProductAvailable_OutsideTimeRange(t *testing.T) {
	// Simule lundi 12h UTC (en dehors de 8h-11h)
	
	schedules := []AvailabilitySchedule{
		{
			DayOfWeek: 2, // Lundi
			StartTime: "08:00:00",
			EndTime:   "11:00:00",
		},
	}
	
	availabilities := []Availability{
		{
			AvailabilityID: "avail-1",
			Schedules:      schedules,
		},
	}
	
	svc := &AvailabilitiesService{
		availabilitiesRepo: &MockRepository{
			availabilities: availabilities,
		},
	}
	
	// Mock time.Now() → lundi 12h
	isAvailable, err := svc.IsProductAvailable(context.Background(), "merchant-1", "product-1")
	
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isAvailable {
		t.Fatal("product should NOT be available at 12:00 (outside 08:00-11:00)")
	}
}

// TestIsProductAvailable_WrongDay tests when it's wrong day of week
func TestIsProductAvailable_WrongDay(t *testing.T) {
	// Simule mardi 9h UTC (but availability only on Monday)
	
	schedules := []AvailabilitySchedule{
		{
			DayOfWeek: 2, // Lundi
			StartTime: "08:00:00",
			EndTime:   "11:00:00",
		},
	}
	
	availabilities := []Availability{
		{
			AvailabilityID: "avail-1",
			Schedules:      schedules,
		},
	}
	
	svc := &AvailabilitiesService{
		availabilitiesRepo: &MockRepository{
			availabilities: availabilities,
		},
	}
	
	// Mock time.Now() → mardi 9h
	isAvailable, err := svc.IsProductAvailable(context.Background(), "merchant-1", "product-1")
	
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isAvailable {
		t.Fatal("product should NOT be available on Tuesday (availability only on Monday)")
	}
}
```

### Test ValidateSchedules

```go
func TestValidateSchedules_ValidInput(t *testing.T) {
	schedules := []CreateAvailabilityScheduleReq{
		{
			DayOfWeek: 2,
			StartTime: "08:00",
			EndTime:   "11:00",
		},
	}
	
	err := validateSchedules(schedules)
	if err != nil {
		t.Fatalf("validation should pass for valid schedules: %v", err)
	}
}

func TestValidateSchedules_InvalidDayOfWeek(t *testing.T) {
	schedules := []CreateAvailabilityScheduleReq{
		{
			DayOfWeek: 8, // Invalid (must be 1-7)
			StartTime: "08:00",
			EndTime:   "11:00",
		},
	}
	
	err := validateSchedules(schedules)
	if err == nil {
		t.Fatal("validation should fail for invalid day_of_week")
	}
}

func TestValidateSchedules_InvalidTimeRange(t *testing.T) {
	schedules := []CreateAvailabilityScheduleReq{
		{
			DayOfWeek: 2,
			StartTime: "11:00",
			EndTime:   "08:00", // End before start
		},
	}
	
	err := validateSchedules(schedules)
	if err == nil {
		t.Fatal("validation should fail when start_time >= end_time")
	}
}
```

---

## Integration Tests

### Test Complete CRUD

```go
// TestAvailabilitiesCRUD tests Create, Read, Update, Delete operations
func TestAvailabilitiesCRUD(t *testing.T) {
	repo := NewAvailabilitiesRepository(db) // Utilise une DB de test
	svc := NewAvailabilitiesService(repo)
	
	ctx := context.Background()
	merchantID := "merchant-test-123"
	
	// 1. CREATE
	createReq := CreateAvailabilityRequest{
		Name: "Test Availability",
		ProductIDs: []string{"prod-1", "prod-2"},
		Schedules: []CreateAvailabilityScheduleReq{
			{DayOfWeek: 2, StartTime: "08:00", EndTime: "11:00"},
		},
	}
	
	ctxWithUser := contextWithUser(ctx, merchantID)
	created, err := svc.CreateAvailability(ctxWithUser, createReq)
	if err != nil {
		t.Fatalf("failed to create availability: %v", err)
	}
	
	availabilityID := created.AvailabilityID
	if availabilityID == "" {
		t.Fatal("created availability should have an ID")
	}
	
	// 2. READ
	read, err := repo.GetAvailabilityByID(context.Background(), merchantID, availabilityID)
	if err != nil {
		t.Fatalf("failed to read availability: %v", err)
	}
	if read.Name != "Test Availability" {
		t.Fatal("read availability should match created data")
	}
	
	// 3. UPDATE
	updateReq := UpdateAvailabilityRequest{
		Name: "Updated Availability",
		ProductIDs: []string{"prod-1", "prod-3"},
		Schedules: []CreateAvailabilityScheduleReq{
			{DayOfWeek: 2, StartTime: "07:00", EndTime: "12:00"},
		},
	}
	
	updated, err := svc.UpdateAvailability(ctxWithUser, availabilityID, updateReq)
	if err != nil {
		t.Fatalf("failed to update availability: %v", err)
	}
	if updated.Name != "Updated Availability" {
		t.Fatal("updated availability should have new name")
	}
	
	// 4. DELETE
	err = svc.DeleteAvailability(ctxWithUser, availabilityID)
	if err != nil {
		t.Fatalf("failed to delete availability: %v", err)
	}
	
	// Verify soft delete (enabled = 0)
	deleted, err := repo.GetAvailabilityByID(context.Background(), merchantID, availabilityID)
	if err == nil && deleted != nil {
		t.Fatal("deleted availability should not be returned")
	}
}
```

---

## Stress Tests

### Test Concurrent Requests

```go
// TestConcurrentCreations tests multiple concurrent create operations
func TestConcurrentCreations(t *testing.T) {
	repo := NewAvailabilitiesRepository(db)
	svc := NewAvailabilitiesService(repo)
	
	ctx := context.Background()
	merchantID := "merchant-stress-test"
	
	numGoroutines := 100
	errors := make(chan error, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			createReq := CreateAvailabilityRequest{
				Name: fmt.Sprintf("Availability %d", index),
				ProductIDs: []string{"prod-1"},
				Schedules: []CreateAvailabilityScheduleReq{
					{DayOfWeek: 2, StartTime: "08:00", EndTime: "11:00"},
				},
			}
			
			ctxWithUser := contextWithUser(ctx, merchantID)
			_, err := svc.CreateAvailability(ctxWithUser, createReq)
			errors <- err
		}(i)
	}
	
	// Collecte les erreurs
	for i := 0; i < numGoroutines; i++ {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent creation failed: %v", err)
		}
	}
	
	// Vérifier que toutes les disponibilités ont été créées
	availabilities, _ := repo.GetAvailabilitiesByMerchant(context.Background(), merchantID)
	if len(availabilities) != numGoroutines {
		t.Fatalf("expected %d availabilities, got %d", numGoroutines, len(availabilities))
	}
}
```

---

## Performance Tests

### Test Query Performance

```go
// BenchmarkGetAvailabilitiesByMerchant measures query performance
func BenchmarkGetAvailabilitiesByMerchant(b *testing.B) {
	repo := NewAvailabilitiesRepository(db)
	merchantID := "merchant-perf-test"
	
	// Setup: Create 1000 availabilities
	for i := 0; i < 1000; i++ {
		createReq := CreateAvailabilityRequest{
			Name: fmt.Sprintf("Avail %d", i),
			ProductIDs: []string{fmt.Sprintf("prod-%d", i)},
			Schedules: []CreateAvailabilityScheduleReq{
				{DayOfWeek: 2, StartTime: "08:00", EndTime: "11:00"},
			},
		}
		repo.Create(context.Background(), merchantID, createReq)
	}
	
	b.ResetTimer()
	
	// Benchmark the query
	for i := 0; i < b.N; i++ {
		repo.GetAvailabilitiesByMerchant(context.Background(), merchantID)
	}
}

// BenchmarkIsProductAvailable measures availability check performance
func BenchmarkIsProductAvailable(b *testing.B) {
	svc := NewAvailabilitiesService(repo)
	ctx := contextWithUser(context.Background(), "merchant-perf-test")
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		svc.IsProductAvailable(ctx, "merchant-perf-test", "prod-1")
	}
}
```

---

## Running Tests

```bash
# Tous les tests
go test ./internal/modules/availabilities/...

# Test spécifique
go test ./internal/modules/availabilities/... -run TestIsProductAvailable_NoAvailabilities

# Avec couverture
go test ./internal/modules/availabilities/... -cover

# Benchmark
go test ./internal/modules/availabilities/... -bench=. -benchmem
```

---

## Mock Repository (pour les tests)

```go
type MockRepository struct {
	availabilities []Availability
}

func (m *MockRepository) GetAvailabilitiesByMerchant(ctx context.Context, merchantID string) ([]Availability, error) {
	return m.availabilities, nil
}

func (m *MockRepository) GetAvailabilityByID(ctx context.Context, merchantID, availabilityID string) (*Availability, error) {
	for _, a := range m.availabilities {
		if a.AvailabilityID == availabilityID {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *MockRepository) Create(ctx context.Context, merchantID string, req CreateAvailabilityRequest) (*Availability, error) {
	// Mock implementation
	return &Availability{AvailabilityID: "test-id"}, nil
}

// ... autres méthodes mock
```

---

**Run tests with: `go test ./internal/modules/availabilities/...`**
