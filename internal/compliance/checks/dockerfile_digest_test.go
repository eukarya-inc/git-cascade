package checks

import (
	"testing"
)

func TestFromLinePattern(t *testing.T) {
	tests := []struct {
		line string
		want string // expected captured image ref, or empty if no match
	}{
		{"FROM golang:1.21", "golang:1.21"},
		{"FROM node:18-alpine", "node:18-alpine"},
		{"FROM ubuntu@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", "ubuntu@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"FROM --platform=linux/amd64 golang:1.21", "golang:1.21"},
		{"FROM scratch", "scratch"},
		{"FROM ${BASE_IMAGE}", "${BASE_IMAGE}"},
		{"RUN echo hello", ""},
	}

	for _, tt := range tests {
		matches := fromLine.FindStringSubmatch(tt.line)
		if tt.want == "" {
			if matches != nil {
				t.Errorf("fromLine(%q): expected no match, got %v", tt.line, matches)
			}
			continue
		}
		if matches == nil {
			t.Errorf("fromLine(%q): expected match, got nil", tt.line)
			continue
		}
		if matches[1] != tt.want {
			t.Errorf("fromLine(%q): got %q, want %q", tt.line, matches[1], tt.want)
		}
	}
}

func TestSHA256DigestPattern(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"ubuntu@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", true},
		{"golang:1.21", false},
		{"node:latest", false},
		{"alpine:3.18", false},
	}

	for _, tt := range tests {
		got := sha256Digest.MatchString(tt.ref)
		if got != tt.want {
			t.Errorf("sha256Digest.MatchString(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}
