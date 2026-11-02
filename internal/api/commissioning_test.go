package api

import (
	"errors"
	"strings"
	"testing"
)

func TestPairingCodeValidation(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{" 3532-533-5079\n", "35325335079"},
		{"123456789012345678901", "123456789012345678901"},
		{"MT:Y.K90AFN00KA0648G00", "MT:Y.K90AFN00KA0648G00"},
		{"secret-invalid-input", ""}, {"MT:", ""}, {"MT:ABC\n123", ""}, {strings.Repeat("1", 1025), ""},
	} {
		got, err := NormalizePairingCode(tt.input)
		if got != tt.want || (err != nil) != (tt.want == "") {
			t.Fatalf("unexpected validation result")
		}
		if err != nil && (!errors.Is(err, ErrInvalidRequest) || (len(tt.input) > 4 && strings.Contains(err.Error(), tt.input))) {
			t.Fatal("invalid input leaked or error unclassified")
		}
	}
	for _, request := range []CommissionRequest{
		{Code: "35325335079"},
		{Code: "35325335079", NetworkOnly: true, TimeoutMS: -1},
		{Code: "35325335079", NetworkOnly: true, TimeoutMS: 999},
		{Code: "35325335079", NetworkOnly: true, TimeoutMS: 600001},
		{Code: "35325335079", NetworkOnly: true, TimeoutMS: 1<<63 - 1},
	} {
		if !errors.Is(request.Validate(), ErrInvalidRequest) {
			t.Fatal("accepted unsupported mode or deadline")
		}
	}
}
