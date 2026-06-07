// Curri API example in Go.
//
//
//  1. Display the authenticated user.
//  2. Get a truck delivery quote between two hardcoded sample addresses.
//  3. Book a delivery using the quote ID from step 2.
//
// The Curri API is GraphQL at https://api.curri.com/graphql and uses only the
// Go standard library here.
//
// Usage:
//
//	go run main.go               # real API (requires CURRI_USER_ID + CURRI_API_KEY)
//	go run main.go -mock         # in-process mock server, no credentials needed
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
)

// endpoint is overwritten by startMockServer when -mock is used.
var endpoint = "https://api.curri.com/graphql"

// graphQLRequest is the JSON body the GraphQL endpoint expects: a query string
// plus an optional map of variables.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphQLError mirrors a single entry in the top-level "errors" array that
// GraphQL returns alongside (or instead of) "data".
type graphQLError struct {
	Message string `json:"message"`
}

// execute POSTs a GraphQL query and unmarshals the "data" field into out.
// authHeader is the full "Basic <token>" Authorization header value.
func execute(authHeader, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decoding response (HTTP %d): %s", resp.StatusCode, raw)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
	}

	return json.Unmarshal(envelope.Data, out)
}

func startMockServer() func() {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(req.Query, "currentUser"):
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"currentUser": map[string]any{
						"id":           "user_MOCK123",
						"emailAddress": "demo@example.com",
					},
				},
			})

		case strings.Contains(req.Query, "deliveryQuote"):
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"deliveryQuote": map[string]any{
						"id":             "quote_MOCKB34GCP",
						"fee":            8450, // $84.50 in cents
						"distance":       3.2,
						"duration":       18.0,
						"deliveryMethod": "truck",
					},
				},
			})

		case strings.Contains(req.Query, "bookDelivery"):
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"bookDelivery": map[string]any{
						"id":             "del_MOCK3XPWMR",
						"price":          8450,
						"deliveryMethod": "truck",
						"deliveryStatus": map[string]any{
							"name": "Pending",
							"code": "pending",
						},
						"trackingUrl": "https://curri.com/track/del_MOCK3XPWMR",
					},
				},
			})

		default:
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{{"message": "unknown operation"}},
			})
		}
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0") // :0 picks a free port
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to start mock server:", err)
		os.Exit(1)
	}
	endpoint = "http://" + ln.Addr().String() + "/graphql"

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	return func() { srv.Close() }
}

func main() {
	mock := flag.Bool("mock", false, "use an in-process mock server instead of the real API")
	flag.Parse()

	var authHeader string

	if *mock {
		fmt.Println("[mock mode] Starting local mock server — no credentials needed.")
		shutdown := startMockServer()
		defer shutdown()
		// Auth header content doesn't matter in mock mode, but we still send one
		// so the request path through execute() is identical to real mode.
		authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte("mock:mock"))
	} else {
		// Credentials come from the environment, never hardcoded.
		userID := os.Getenv("CURRI_USER_ID")
		apiKey := os.Getenv("CURRI_API_KEY")
		if userID == "" || apiKey == "" {
			fmt.Fprintln(os.Stderr, "Please set CURRI_USER_ID and CURRI_API_KEY environment variables.")
			fmt.Fprintln(os.Stderr, "Or run with -mock to use the local mock server.")
			os.Exit(1)
		}
		authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(userID+":"+apiKey))
	}

	// --- Step 1: display the authenticated user ---------------------------
	var userResp struct {
		CurrentUser struct {
			ID           string `json:"id"`
			EmailAddress string `json:"emailAddress"`
		} `json:"currentUser"`
	}
	const userQuery = `
		query {
			currentUser {
				id
				emailAddress
			}
		}`
	if err := execute(authHeader, userQuery, nil, &userResp); err != nil {
		fmt.Fprintln(os.Stderr, "Step 1 (current user) failed:", err)
		os.Exit(1)
	}
	if userResp.CurrentUser.ID == "" {
		fmt.Fprintln(os.Stderr, "We were unable to authenticate you.")
		os.Exit(1)
	}
	fmt.Printf("Logged in as: %s (%s)\n", userResp.CurrentUser.EmailAddress, userResp.CurrentUser.ID)

	// --- Step 2: get a truck quote between two sample addresses -----------
	// Curri's quote-then-book flow: first ask for a quote, which returns an id
	// and a fee. That id is then handed to bookDelivery. Addresses are inlined
	// here to mirror the other language examples; the fee is in USD cents.
	var quoteResp struct {
		DeliveryQuote struct {
			ID             string  `json:"id"`
			Fee            int     `json:"fee"` // USD cents
			Distance       float64 `json:"distance"`
			Duration       float64 `json:"duration"`
			DeliveryMethod string  `json:"deliveryMethod"`
		} `json:"deliveryQuote"`
	}
	const quoteQuery = `
		query {
			deliveryQuote(
				origin: {
					name: "305 South Kalorama Street"
					addressLine1: "305 S Kalorama St"
					city: "Ventura"
					state: "CA"
					postalCode: "93001"
				}
				destination: {
					name: "Curri Incubator"
					addressLine1: "54 S Oak St."
					city: "Ventura"
					state: "CA"
					postalCode: "93001"
				}
				deliveryMethod: "truck"
			) {
				id
				fee
				distance
				duration
				deliveryMethod
			}
		}`
	if err := execute(authHeader, quoteQuery, nil, &quoteResp); err != nil {
		fmt.Fprintln(os.Stderr, "Step 2 (quote) failed:", err)
		os.Exit(1)
	}
	q := quoteResp.DeliveryQuote
	fmt.Printf("Quote %s: $%.2f (%s, %.1f mi)\n", q.ID, float64(q.Fee)/100, q.DeliveryMethod, q.Distance)

	// --- Step 3: book a delivery using the quote ID ----------------------
	// Quotes expire 15 minutes after they are issued, so booking should happen
	// promptly after the quote is returned. The quote ID is server-generated,
	// so we pass it through a GraphQL variable.
	var bookResp struct {
		BookDelivery struct {
			ID             string `json:"id"`
			Price          int    `json:"price"` // USD cents
			DeliveryMethod string `json:"deliveryMethod"`
			DeliveryStatus struct {
				Name string `json:"name"`
				Code string `json:"code"`
			} `json:"deliveryStatus"`
			TrackingURL string `json:"trackingUrl"`
		} `json:"bookDelivery"`
	}
	const bookMutation = `
		mutation BookDelivery($deliveryQuoteId: String!) {
			bookDelivery(data: { deliveryQuoteId: $deliveryQuoteId }) {
				id
				price
				deliveryMethod
				deliveryStatus {
					name
					code
				}
				trackingUrl
			}
		}`
	bookVars := map[string]any{"deliveryQuoteId": q.ID}
	if err := execute(authHeader, bookMutation, bookVars, &bookResp); err != nil {
		fmt.Fprintln(os.Stderr, "Step 3 (book) failed:", err)
		os.Exit(1)
	}
	d := bookResp.BookDelivery
	fmt.Printf("Booked delivery %s: status %q (%s), $%.2f\n", d.ID, d.DeliveryStatus.Name, d.DeliveryStatus.Code, float64(d.Price)/100)
	if d.TrackingURL != "" {
		fmt.Println("Track it at:", d.TrackingURL)
	}
}
