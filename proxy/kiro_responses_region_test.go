package proxy

import (
	"testing"

	"kiro-go/config"
)

func TestResponsesEndpointUsesProfileRegionNotAuthRegion(t *testing.T) {
	acc := &config.Account{
		Region:     "us-east-1",
		ProfileArn: "arn:aws:codewhisperer:eu-central-1:123456789012:profile/ABC",
	}
	if got, want := responsesEndpointURL(acc), "https://runtime.eu-central-1.kiro.dev/v1/responses"; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if got, want := responsesEndpointURL(&config.Account{Region: "ap-southeast-2"}), "https://runtime.us-east-1.kiro.dev/v1/responses"; got != want {
		t.Fatalf("OAuth auth region must not pick the data plane: got %s want %s", got, want)
	}
}
