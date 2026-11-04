package api

import "testing"

func TestLightRequestValidation(t *testing.T) {
	for _, request := range []LightRequest{
		{Target: "1", Endpoint: 1, Action: "brightness", Value: 50},
		{Target: "1", Endpoint: 1, Action: "temperature", Value: 5000},
		{Target: "1", Endpoint: 1, Action: "color", Color: "#ff0088"},
	} {
		if err := request.Validate(); err != nil {
			t.Errorf("valid request %+v: %v", request, err)
		}
	}
	for _, request := range []LightRequest{
		{Target: "1", Endpoint: 0, Action: "brightness", Value: 50},
		{Target: "1", Endpoint: 1, Action: "brightness", Value: 0},
		{Target: "1", Endpoint: 1, Action: "temperature", Value: 999},
		{Target: "1", Endpoint: 1, Action: "color", Color: "red"},
		{Target: "1", Endpoint: 1, Action: "color", Color: "#ff0000", Value: 1},
	} {
		if err := request.Validate(); err == nil {
			t.Errorf("invalid request accepted: %+v", request)
		}
	}
}
