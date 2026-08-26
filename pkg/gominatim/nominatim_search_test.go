/*
 *    Copyright (C) 2014 Daniel 'grindhold' Brendle
 *
 *    This program is free software: you can redistribute it and/or modify
 *    it under the terms of the GNU Lesser General Public License as published
 *    by the Free Software Foundation, either version 3 of the License, or
 *    (at your option) any later version.
 *
 *    This program is distributed in the hope that it will be useful,
 *    but WITHOUT ANY WARRANTY; without even the implied warranty of
 *    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *    GNU Lesser General Public License for more details.
 *
 *    You should have received a copy of the GNU Lesser General Public License
 *    along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 *    Authors:
 *      Daniel 'grindhold' Brendle <grindhold@skarphed.org>
 */

package gominatim

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchQueryGetDecodesImportance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"place_id":1,"display_name":"Berlin","lat":"52.5","lon":"13.4","importance":0.75}]`))
	}))
	t.Cleanup(server.Close)
	SetServer(server.URL)
	t.Cleanup(func() { SetServer("") })

	results, err := (&SearchQuery{Q: "Berlin"}).Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Get() returned %d results, want 1", len(results))
	}
	if results[0].Importance != 0.75 {
		t.Errorf("Importance = %v, want 0.75", results[0].Importance)
	}
}

func TestSearchQueryGetReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"bad query"}`))
	}))
	t.Cleanup(server.Close)
	SetServer(server.URL)
	t.Cleanup(func() { SetServer("") })

	_, err := (&SearchQuery{Q: "invalid"}).Get()
	if err == nil || !strings.Contains(err.Error(), "nominatim error: bad query") {
		t.Fatalf("Get() error = %v, want Nominatim API error", err)
	}
}

func Test_CreateSearchQuery(t *testing.T) {
	defer SetServer("")
	SetServer("https://nominatim.openstreetmap.org")
	expectation := "q=Berlin"
	q := new(SearchQuery)
	q.Q = "Berlin"
	qstr, err := q.buildQuery()
	if !strings.Contains(qstr, expectation) {
		t.Errorf("resulting query should contain %s", expectation)
	}
	if err != nil {
		t.Errorf("triggered error that was not supposed to: %s", err.Error())
	}
}

func Test_CreateSearchQueryWithParams(t *testing.T) {
	defer SetServer("")
	SetServer("https://nominatim.openstreetmap.org")
	expectations := []string{
		"city=Berlin",
		"street=Karl-Marx-Allee",
		"county=Berlin",
		"state=Germany",
		"postalcode=012345",
	}
	q := &SearchQuery{
		City:       "Berlin",
		Street:     "Karl-Marx-Allee",
		County:     "Berlin",
		State:      "Germany",
		Postalcode: "012345",
	}
	qstr, err := q.buildQuery()
	for i := range expectations {
		if !strings.Contains(qstr, expectations[i]) {
			t.Errorf("resulting query should contain %s", expectations[i])
		}
	}
	if err != nil {
		t.Errorf("triggered error that was not supposed to: %s", err.Error())
	}
}

func Test_SpecificFieldsUsed(t *testing.T) {
	defer SetServer("")
	SetServer("https://nominatim.openstreetmap.org")
	q1 := &SearchQuery{
		City:       "Berlin",
		Street:     "Karl-Marx-Allee",
		County:     "Berlin",
		State:      "Germany",
		Postalcode: "012345",
	}
	q2 := new(SearchQuery)
	q2.Q = "Berlin"
	if !q1.specificFieldsUsed() {
		t.Error("Q1 -> specific fields are used. should return true")
	}
	if q2.specificFieldsUsed() {
		t.Error("Q2 -> specific fields are not used. should return false")
	}
}

func Test_EmptySearchQuery(t *testing.T) {
	defer SetServer("")
	SetServer("https://nominatim.openstreetmap.org")
	q := new(SearchQuery)
	_, err := q.buildQuery()
	if err == nil {
		t.Error("Empty query should result in an error")
	}
}

func Test_DoubleSearchQuery(t *testing.T) {
	defer SetServer("")
	SetServer("https://nominatim.openstreetmap.org")
	q := &SearchQuery{
		City:       "Berlin",
		Street:     "Karl-Marx-Allee",
		County:     "Berlin",
		State:      "Germany",
		Postalcode: "012345",
		Q:          "Berlin",
	}
	expectations := []string{
		"city=Berlin",
		"street=Karl-Marx-Allee",
		"county=Berlin",
		"state=Germany",
		"postalcode=012345",
	}
	qstr, err := q.buildQuery()
	for i := range expectations {
		if strings.Contains(qstr, expectations[i]) {
			t.Errorf("query should not contain %s", expectations[i])
		}
	}
	if !strings.Contains(qstr, "q=Berlin") {
		t.Error("query should contain q=Berlin")
	}
	if err != nil {
		t.Error("should not throw error")
	}
}

func Test_LimitedSearchQuery(t *testing.T) {
	defer SetServer("")
	SetServer("https://nominatim.openstreetmap.org")
	expectation := "limit=123"
	q := new(SearchQuery)
	q.Q = "Berlin"
	q.Limit = 123
	qstr, err := q.buildQuery()
	if !strings.Contains(qstr, expectation) {
		t.Errorf("resulting query should contain %s", expectation)
	}
	if err != nil {
		t.Errorf("triggered error that was not supposed to: %s", err.Error())
	}
}

func Test_AddressFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"display_name":"Unter den Linden","lat":"52.5","lon":"13.4","address":{"suburb":"Mitte","state":"Berlin"}}]`))
	}))
	t.Cleanup(server.Close)
	SetServer(server.URL)
	t.Cleanup(func() { SetServer("") })

	resp, err := (&SearchQuery{Q: "Unter den Linden"}).Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("Get() returned %d results, want 1", len(resp))
	}
	if resp[0].Address.Suburb != "Mitte" {
		t.Errorf("Address.Suburb = %q, want Mitte", resp[0].Address.Suburb)
	}
	if resp[0].Address.State != "Berlin" {
		t.Errorf("Address.State = %q, want Berlin", resp[0].Address.State)
	}
}
