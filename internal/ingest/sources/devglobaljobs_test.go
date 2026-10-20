package sources

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestDevGlobalJobsProvider(t *testing.T) {
	if got := NewDevGlobalJobs(nil).Provider(); got != "devglobaljobs" {
		t.Fatalf("Provider() = %q, want %q", got, "devglobaljobs")
	}
}

func TestDevGlobalJobsMarkers(t *testing.T) {
	s := NewDevGlobalJobs(nil)
	if _, ok := s.(boardless); !ok {
		t.Error("devglobaljobs should implement boardless")
	}
	if _, ok := s.(aggregator); !ok {
		t.Error("devglobaljobs should implement aggregator")
	}
	if _, ok := s.(CoverageGated); !ok {
		t.Error("devglobaljobs should implement CoverageGated")
	}
	if gs, ok := s.(sweepGrace); !ok || gs.sweepGrace() != 14*24*time.Hour {
		t.Errorf("devglobaljobs sweepGrace() = %v, want 14 days", gs.sweepGrace())
	}
}

func TestDevGlobalJobsFetch(t *testing.T) {
	listJSON := `{
		"jobs": [
			{
				"id": 165923983,
				"title": "Senior Software Engineer, Storage",
				"organization": "Airbnb",
				"location": "San Francisco, CA",
				"country": "US",
				"category": "technology",
				"jobType": "Full-time",
				"descriptionExcerpt": "Short excerpt here",
				"postedAt": "2026-10-20T05:41:25.987Z",
				"url": "https://devglobaljobs.com/jobs/detail/165923983"
			},
			{
				"id": 165916167,
				"title": "Full Stack Developer",
				"organization": "RemoteCo",
				"location": "Remote",
				"country": "GB",
				"category": "remote",
				"jobType": "Contract",
				"descriptionExcerpt": "Remote work only",
				"postedAt": "2026-10-20T05:11:28.119Z",
				"url": "https://devglobaljobs.com/jobs/detail/165916167"
			}
		]
	}`

	detailJSON := `{
		"id": 165923983,
		"description": "<p>Full detailed description from API</p>"
	}`

	routed := (&routedHTTP{}).
		route("offset=0", listJSON).
		route("/api/v1/jobs/165923983", detailJSON)

	jobs, err := NewDevGlobalJobs(routed).Fetch(context.Background(), CompanyEntry{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2", len(jobs))
	}

	// First job has detailed description and country mapped
	j1 := jobs[0]
	if j1.ExternalID != "165923983" {
		t.Errorf("ExternalID = %q, want 165923983", j1.ExternalID)
	}
	if j1.Title != "Senior Software Engineer, Storage" {
		t.Errorf("Title = %q", j1.Title)
	}
	if j1.Company != "Airbnb" {
		t.Errorf("Company = %q", j1.Company)
	}
	if j1.Location != "San Francisco, CA" {
		t.Errorf("Location = %q", j1.Location)
	}
	if !reflect.DeepEqual(j1.Countries, []string{"us"}) {
		t.Errorf("Countries = %v, want [us]", j1.Countries)
	}
	if j1.EmploymentType != "full_time" {
		t.Errorf("EmploymentType = %q, want full_time", j1.EmploymentType)
	}
	if j1.Description != "<p>Full detailed description from API</p>" {
		t.Errorf("Description = %q", j1.Description)
	}
	if j1.PostedAt == nil {
		t.Error("PostedAt is nil")
	}

	// Second job fell back to DescriptionExcerpt because no detail route was mocked, and has Remote / Contract mapped
	j2 := jobs[1]
	if j2.ExternalID != "165916167" {
		t.Errorf("ExternalID = %q, want 165916167", j2.ExternalID)
	}
	if !j2.Remote || j2.WorkMode != "remote" {
		t.Errorf("Remote=%v, WorkMode=%q, want remote", j2.Remote, j2.WorkMode)
	}
	if j2.EmploymentType != "contract" {
		t.Errorf("EmploymentType = %q, want contract", j2.EmploymentType)
	}
	if !reflect.DeepEqual(j2.Countries, []string{"gb"}) {
		t.Errorf("Countries = %v, want [gb]", j2.Countries)
	}
	if j2.Description != "Remote work only" {
		t.Errorf("Description = %q, want 'Remote work only'", j2.Description)
	}
}

func TestDevGlobalJobsPagination(t *testing.T) {
	// Build a full 100-job page 0
	page0JSON := `{"meta":{"hasMore":true},"jobs":[`
	for i := 0; i < 100; i++ {
		if i > 0 {
			page0JSON += ","
		}
		page0JSON += fmt.Sprintf(`{"id":%d,"title":"Job %d","organization":"Org %d","url":"https://devglobaljobs.com/jobs/detail/%d"}`, i+1, i+1, i+1, i+1)
	}
	page0JSON += `]}`

	// Page 1 with 2 jobs and hasMore: false
	page1JSON := `{
		"meta": {"hasMore": false},
		"jobs": [
			{"id": 101, "title": "Job 101", "organization": "Org 101", "url": "https://devglobaljobs.com/jobs/detail/101"},
			{"id": 102, "title": "Job 102", "organization": "Org 102", "url": "https://devglobaljobs.com/jobs/detail/102"}
		]
	}`

	routed := (&routedHTTP{}).
		route("offset=0", page0JSON).
		route("offset=100", page1JSON)

	jobs, err := NewDevGlobalJobs(routed).Fetch(context.Background(), CompanyEntry{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(jobs) != 102 {
		t.Fatalf("len(jobs) = %d, want 102 across two pages", len(jobs))
	}
	if jobs[0].ExternalID != "1" || jobs[101].ExternalID != "102" {
		t.Errorf("unexpected external IDs: first=%s, last=%s", jobs[0].ExternalID, jobs[101].ExternalID)
	}
}

func TestDevGlobalJobsFetchNewSeen(t *testing.T) {
	listJSON := `{
		"jobs": [
			{
				"id": 12345,
				"title": "Backend Engineer",
				"organization": "Stripe",
				"url": "https://devglobaljobs.com/jobs/detail/12345"
			}
		]
	}`

	routed := (&routedHTTP{}).route("offset=0", listJSON)

	// Mark 12345 as seen: should emit SeenRefresh with zero detail calls
	seenMap := map[string]bool{"12345": true}
	jobs, err := NewDevGlobalJobs(routed).(HydratingSource).FetchNew(context.Background(), CompanyEntry{}, func(id string) bool {
		return seenMap[id]
	})
	if err != nil {
		t.Fatalf("FetchNew: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if !jobs[0].SeenRefresh {
		t.Errorf("expected SeenRefresh to be true, got false")
	}
	if jobs[0].ExternalID != "12345" || jobs[0].Company != "Stripe" {
		t.Errorf("unexpected job: %+v", jobs[0])
	}
}

func TestDevGlobalJobsCoverageGated(t *testing.T) {
	listJSON := `{
		"jobs": [
			{
				"id": 101,
				"title": "Software Engineer",
				"organization": "OpenAI",
				"descriptionExcerpt": "OpenAI excerpt",
				"url": "https://devglobaljobs.com/jobs/detail/101"
			},
			{
				"id": 102,
				"title": "Chef",
				"organization": "Local Bistro",
				"descriptionExcerpt": "Bistro excerpt",
				"url": "https://devglobaljobs.com/jobs/detail/102"
			},
			{
				"id": 103,
				"title": "Go Developer",
				"organization": "UncoveredStartup",
				"descriptionExcerpt": "Startup excerpt",
				"url": "https://devglobaljobs.com/jobs/detail/103"
			}
		]
	}`

	detailJSON := `{
		"id": 103,
		"description": "<p>Hydrated full body for uncovered startup</p>"
	}`

	routed := (&routedHTTP{}).
		route("offset=0", listJSON).
		route("/api/v1/jobs/103", detailJSON)

	gate := DetailGate{
		Covered: func(companies []string) map[string]bool {
			return map[string]bool{
				"OpenAI": true,
			}
		},
		Rejected: func(j Job) bool {
			return j.Title == "Chef"
		},
	}

	jobs, err := NewDevGlobalJobs(routed).(CoverageGated).FetchNewGated(context.Background(), CompanyEntry{}, func(string) bool { return false }, gate)
	if err != nil {
		t.Fatalf("FetchNewGated: %v", err)
	}

	if len(jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3", len(jobs))
	}

	// 101 (OpenAI) was covered: yielded body-less (uses excerpt, no detail fetch called)
	if jobs[0].Company != "OpenAI" || jobs[0].Description != "OpenAI excerpt" {
		t.Errorf("unexpected job 0: %+v", jobs[0])
	}

	// 102 (Chef) was rejected by filter: yielded body-less (uses excerpt, no detail fetch called)
	if jobs[1].Company != "Local Bistro" || jobs[1].Description != "Bistro excerpt" {
		t.Errorf("unexpected job 1: %+v", jobs[1])
	}

	// 103 (UncoveredStartup) was neither covered nor rejected: detail fetch was called!
	if jobs[2].Company != "UncoveredStartup" || jobs[2].Description != "<p>Hydrated full body for uncovered startup</p>" {
		t.Errorf("unexpected job 2: %+v", jobs[2])
	}
}

func TestDevGlobalJobsListingError(t *testing.T) {
	fake := &fakeHTTP{err: errors.New("network down")}
	_, err := NewDevGlobalJobs(fake).Fetch(context.Background(), CompanyEntry{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
