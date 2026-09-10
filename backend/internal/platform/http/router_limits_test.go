package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

type boundedInput struct {
	Body struct {
		A string `json:"a" required:"true"`
		B string `json:"b" required:"true"`
		C string `json:"c" required:"true"`
		D string `json:"d" required:"true"`
		E string `json:"e" required:"true"`
		F string `json:"f" required:"true"`
		G string `json:"g" required:"true"`
		H string `json:"h" required:"true"`
		I string `json:"i" required:"true"`
		J string `json:"j" required:"true"`
		K string `json:"k" required:"true"`
	}
}

type boundedOutput struct{ Body map[string]string }

func TestRouterBoundsRequestBodiesAndValidationDetails(t *testing.T) {
	router, api := platformhttp.NewRouter("bounded transport", "test")
	huma.Register(api, huma.Operation{
		OperationID: "bounded-transport-test", Method: http.MethodPost, Path: "/bounded",
	}, func(context.Context, *boundedInput) (*boundedOutput, error) {
		return &boundedOutput{Body: map[string]string{"status": "ok"}}, nil
	})

	t.Run("one byte over the body limit is 413", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/bounded",
			strings.NewReader(strings.Repeat("x", procurementlimits.MaxJSONBodyBytes+1)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413: %s", response.Code, response.Body.String())
		}
	})

	t.Run("validation details stop at ten", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/bounded", bytes.NewBufferString(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", response.Code, response.Body.String())
		}
		var body struct {
			Errors []json.RawMessage `json:"errors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode validation error: %v", err)
		}
		if len(body.Errors) != procurementlimits.MaxErrorDetails {
			t.Errorf("validation details = %d, want %d", len(body.Errors), procurementlimits.MaxErrorDetails)
		}
	})
}
