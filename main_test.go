package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withEndpoint temporarily overrides the package-level endpoint for the
// duration of a test and restores it on cleanup.
func withEndpoint(t *testing.T, url string) {
	t.Helper()
	orig := endpoint
	endpoint = url
	t.Cleanup(func() { endpoint = orig })
}

// serve returns an httptest.Server that always responds with the given JSON
// body and registers cleanup on t.
func serve(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- Auth header -----------------------------------------------------------

func TestAuthHeaderEncoding(t *testing.T) {
	// The Curri docs require base64("userID:apiKey") prefixed with "Basic ".
	token := base64.StdEncoding.EncodeToString([]byte("myUser:myKey"))
	got := "Basic " + token
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("myUser:myKey"))
	if got != want {
		t.Errorf("auth header = %q, want %q", got, want)
	}
	// Spot-check the decoded value to make the test self-documenting.
	decoded, _ := base64.StdEncoding.DecodeString(token)
	if string(decoded) != "myUser:myKey" {
		t.Errorf("decoded token = %q, want %q", decoded, "myUser:myKey")
	}
}

// --- execute() unit tests --------------------------------------------------

func TestExecute_Success(t *testing.T) {
	srv := serve(t, `{"data":{"currentUser":{"id":"u1","emailAddress":"a@b.com"}}}`)
	withEndpoint(t, srv.URL)

	var out struct {
		CurrentUser struct {
			ID           string `json:"id"`
			EmailAddress string `json:"emailAddress"`
		} `json:"currentUser"`
	}
	if err := execute("Basic dGVzdA==", `{ currentUser { id emailAddress } }`, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.CurrentUser.ID != "u1" {
		t.Errorf("id = %q, want %q", out.CurrentUser.ID, "u1")
	}
	if out.CurrentUser.EmailAddress != "a@b.com" {
		t.Errorf("emailAddress = %q, want %q", out.CurrentUser.EmailAddress, "a@b.com")
	}
}

func TestExecute_GraphQLError(t *testing.T) {
	srv := serve(t, `{"errors":[{"message":"not authorized"}]}`)
	withEndpoint(t, srv.URL)

	var out any
	err := execute("Basic dGVzdA==", `{ currentUser { id } }`, nil, &out)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("error %q should mention 'not authorized'", err)
	}
}

func TestExecute_InvalidJSON(t *testing.T) {
	srv := serve(t, `not json at all`)
	withEndpoint(t, srv.URL)

	var out any
	err := execute("Basic dGVzdA==", `{ currentUser { id } }`, nil, &out)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExecute_ForwardsVariables(t *testing.T) {
	var received graphQLRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)

	vars := map[string]any{"deliveryQuoteId": "quote_ABC"}
	var out any
	execute("Basic dGVzdA==", `mutation BookDelivery($deliveryQuoteId: String!) { bookDelivery(data:{deliveryQuoteId:$deliveryQuoteId}){id} }`, vars, &out)

	got, _ := received.Variables["deliveryQuoteId"].(string)
	if got != "quote_ABC" {
		t.Errorf("variable deliveryQuoteId = %q, want %q", got, "quote_ABC")
	}
}

func TestExecute_SetsAuthHeader(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)
	withEndpoint(t, srv.URL)

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user1:key1"))
	var out any
	execute(wantAuth, `{ currentUser { id } }`, nil, &out)

	if receivedAuth != wantAuth {
		t.Errorf("Authorization header = %q, want %q", receivedAuth, wantAuth)
	}
}

// --- mock server tests -----------------------------------------------------

func TestMockServer_CurrentUser(t *testing.T) {
	shutdown := startMockServer()
	t.Cleanup(shutdown)

	var out struct {
		CurrentUser struct {
			ID           string `json:"id"`
			EmailAddress string `json:"emailAddress"`
		} `json:"currentUser"`
	}
	if err := execute("Basic dGVzdA==", `query { currentUser { id emailAddress } }`, nil, &out); err != nil {
		t.Fatalf("currentUser: %v", err)
	}
	if out.CurrentUser.ID == "" {
		t.Error("expected a non-empty currentUser.id")
	}
	if out.CurrentUser.EmailAddress == "" {
		t.Error("expected a non-empty currentUser.emailAddress")
	}
}

func TestMockServer_DeliveryQuote(t *testing.T) {
	shutdown := startMockServer()
	t.Cleanup(shutdown)

	var out struct {
		DeliveryQuote struct {
			ID             string  `json:"id"`
			Fee            int     `json:"fee"`
			Distance       float64 `json:"distance"`
			DeliveryMethod string  `json:"deliveryMethod"`
		} `json:"deliveryQuote"`
	}
	if err := execute("Basic dGVzdA==", `query { deliveryQuote { id fee distance deliveryMethod } }`, nil, &out); err != nil {
		t.Fatalf("deliveryQuote: %v", err)
	}
	if out.DeliveryQuote.ID == "" {
		t.Error("expected a non-empty deliveryQuote.id")
	}
	if out.DeliveryQuote.Fee <= 0 {
		t.Errorf("fee = %d, want > 0", out.DeliveryQuote.Fee)
	}
	// Fee is in USD cents; sanity-check the display value.
	dollars := float64(out.DeliveryQuote.Fee) / 100
	if dollars <= 0 {
		t.Errorf("fee in dollars = %.2f, want > 0", dollars)
	}
}

func TestMockServer_BookDelivery(t *testing.T) {
	shutdown := startMockServer()
	t.Cleanup(shutdown)

	var out struct {
		BookDelivery struct {
			ID             string `json:"id"`
			Price          int    `json:"price"`
			DeliveryStatus struct {
				Name string `json:"name"`
				Code string `json:"code"`
			} `json:"deliveryStatus"`
			TrackingURL string `json:"trackingUrl"`
		} `json:"bookDelivery"`
	}
	vars := map[string]any{"deliveryQuoteId": "quote_MOCKB34GCP"}
	if err := execute("Basic dGVzdA==", `mutation BookDelivery($deliveryQuoteId: String!) { bookDelivery(data:{deliveryQuoteId:$deliveryQuoteId}){ id price deliveryStatus { name code } trackingUrl } }`, vars, &out); err != nil {
		t.Fatalf("bookDelivery: %v", err)
	}
	if out.BookDelivery.ID == "" {
		t.Error("expected a non-empty bookDelivery.id")
	}
	if out.BookDelivery.DeliveryStatus.Code == "" {
		t.Error("expected a non-empty deliveryStatus.code")
	}
	if !strings.HasPrefix(out.BookDelivery.TrackingURL, "https://") {
		t.Errorf("trackingUrl = %q, want https:// prefix", out.BookDelivery.TrackingURL)
	}
}

func TestMockServer_UnknownOperation(t *testing.T) {
	shutdown := startMockServer()
	t.Cleanup(shutdown)

	var out any
	err := execute("Basic dGVzdA==", `query { unknownField { id } }`, nil, &out)
	if err == nil {
		t.Fatal("expected an error for an unknown operation, got nil")
	}
}

// TestFullMockFlow runs all three steps in sequence, threading the quote ID
// into the booking step — the same flow that main() runs.
func TestFullMockFlow(t *testing.T) {
	shutdown := startMockServer()
	t.Cleanup(shutdown)

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("mock:mock"))

	// Step 1: current user.
	var userOut struct {
		CurrentUser struct {
			ID string `json:"id"`
		} `json:"currentUser"`
	}
	if err := execute(authHeader, `query { currentUser { id emailAddress } }`, nil, &userOut); err != nil {
		t.Fatalf("step 1 (currentUser): %v", err)
	}
	if userOut.CurrentUser.ID == "" {
		t.Fatal("step 1: got empty user id")
	}
	t.Logf("step 1 OK: user %s", userOut.CurrentUser.ID)

	// Step 2: delivery quote.
	var quoteOut struct {
		DeliveryQuote struct {
			ID  string `json:"id"`
			Fee int    `json:"fee"`
		} `json:"deliveryQuote"`
	}
	if err := execute(authHeader, `query { deliveryQuote { id fee distance duration deliveryMethod } }`, nil, &quoteOut); err != nil {
		t.Fatalf("step 2 (deliveryQuote): %v", err)
	}
	if quoteOut.DeliveryQuote.ID == "" {
		t.Fatal("step 2: got empty quote id")
	}
	t.Logf("step 2 OK: quote %s ($%.2f)", quoteOut.DeliveryQuote.ID, float64(quoteOut.DeliveryQuote.Fee)/100)

	// Step 3: book using the quote ID from step 2.
	var bookOut struct {
		BookDelivery struct {
			ID             string `json:"id"`
			DeliveryStatus struct {
				Code string `json:"code"`
			} `json:"deliveryStatus"`
		} `json:"bookDelivery"`
	}
	vars := map[string]any{"deliveryQuoteId": quoteOut.DeliveryQuote.ID}
	if err := execute(authHeader, `mutation BookDelivery($deliveryQuoteId: String!) { bookDelivery(data:{deliveryQuoteId:$deliveryQuoteId}){ id price deliveryStatus { name code } trackingUrl } }`, vars, &bookOut); err != nil {
		t.Fatalf("step 3 (bookDelivery): %v", err)
	}
	if bookOut.BookDelivery.ID == "" {
		t.Fatal("step 3: got empty delivery id")
	}
	t.Logf("step 3 OK: delivery %s (status: %s)", bookOut.BookDelivery.ID, bookOut.BookDelivery.DeliveryStatus.Code)
}
