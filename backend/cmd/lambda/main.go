package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/matthewyoungbar/swim-attendance-app/internal/auth"
	"github.com/matthewyoungbar/swim-attendance-app/internal/calendar"
	"github.com/matthewyoungbar/swim-attendance-app/internal/db"
	"github.com/matthewyoungbar/swim-attendance-app/internal/handlers"
)

// LambdaFunctionURLRequest matches the Lambda Function URL payload v2 format.
type LambdaFunctionURLRequest struct {
	Version         string            `json:"version"`
	RouteKey        string            `json:"routeKey"`
	RawPath         string            `json:"rawPath"`
	RawQueryString  string            `json:"rawQueryString"`
	Headers         map[string]string `json:"headers"`
	RequestContext  RequestContext    `json:"requestContext"`
	Body            string            `json:"body"`
	IsBase64Encoded bool              `json:"isBase64Encoded"`
}

type RequestContext struct {
	HTTP HTTPContext `json:"http"`
}

type HTTPContext struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	Protocol  string `json:"protocol"`
	SourceIP  string `json:"sourceIp"`
	UserAgent string `json:"userAgent"`
}

type LambdaFunctionURLResponse struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

func main() {
	ctx := context.Background()

	dbClient, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to init DynamoDB: %v", err)
	}

	var calClient *calendar.Client
	if os.Getenv("GOOGLE_CALENDAR_ID") != "" {
		calClient, err = calendar.NewClient(ctx)
		if err != nil {
			log.Printf("WARNING: Calendar client not initialized: %v", err)
		}
	}

	wa, err := auth.NewWebAuthn()
	if err != nil {
		log.Fatalf("Failed to init WebAuthn: %v", err)
	}

	h := handlers.New(dbClient, calClient, wa)

	// Local dev mode
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		log.Printf("Starting local server on :%s", port)
		if err := http.ListenAndServe(":"+port, h); err != nil {
			log.Fatalf("Server error: %v", err)
		}
		return
	}

	lambda.Start(func(ctx context.Context, req LambdaFunctionURLRequest) (LambdaFunctionURLResponse, error) {
		return adaptRequest(h, req), nil
	})
}

func adaptRequest(h http.Handler, req LambdaFunctionURLRequest) LambdaFunctionURLResponse {
	url := req.RawPath
	if req.RawQueryString != "" {
		url += "?" + req.RawQueryString
	}

	httpReq, err := http.NewRequest(req.RequestContext.HTTP.Method, url, strings.NewReader(req.Body))
	if err != nil {
		return LambdaFunctionURLResponse{StatusCode: 500, Body: `{"success":false,"error":"internal error"}`}
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	rw := &responseWriter{headers: make(http.Header), statusCode: 200}
	h.ServeHTTP(rw, httpReq)

	respHeaders := map[string]string{}
	for k := range rw.headers {
		respHeaders[k] = rw.headers.Get(k)
	}

	return LambdaFunctionURLResponse{
		StatusCode: rw.statusCode,
		Headers:    respHeaders,
		Body:       rw.body.String(),
	}
}

type responseWriter struct {
	headers    http.Header
	statusCode int
	body       bytes.Buffer
}

func (rw *responseWriter) Header() http.Header         { return rw.headers }
func (rw *responseWriter) WriteHeader(status int)      { rw.statusCode = status }
func (rw *responseWriter) Write(b []byte) (int, error) { return rw.body.Write(b) }
