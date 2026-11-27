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

func TestDockerfileDigestChecker_ID(t *testing.T) {
	c := &dockerfileDigestChecker{}
	if c.ID() != "dockerfile-digest" {
		t.Errorf("unexpected ID: %q", c.ID())
	}
}

func TestIsDockerfile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"Dockerfile", true},
		{"web/Dockerfile", true},
		{"cms/web/Dockerfile", true},
		{"dockerfile", true},
		{"Dockerfile.dev", true},
		{"Dockerfile-prod", true},
		{"Dockerfile_ci", true},
		{"server/api.Dockerfile", true},
		{"Containerfile", true},
		{"Dockerfile.dockerignore", false},
		{"internal/compliance/checks/dockerfile_digest.go", false},
		{"dockerfile_test.go", false},
		{"docs/Dockerfile.md", false},
		{"dockerfile-guide.md", false},
		{"Dockerfile.tmpl", true},
		{"Dockerfiles", false},
		{"docs/dockerfile-guide/README.md", false},
		{"main.go", false},
	}
	for _, tt := range tests {
		if got := isDockerfile(tt.path); got != tt.want {
			t.Errorf("isDockerfile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestSkipDockerfileDir(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"Dockerfile", false},
		{"web/Dockerfile", false},
		{"node_modules/pkg/Dockerfile", true},
		{"web/node_modules/pkg/Dockerfile", true},
		{"vendor/github.com/x/y/Dockerfile", true},
		{"vendors/Dockerfile", false},
		{"third_party/x/Dockerfile", true},
		{"internal/testdata/Dockerfile", true},
	}
	for _, tt := range tests {
		if got := skipDockerfileDir(tt.path); got != tt.want {
			t.Errorf("skipDockerfileDir(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFindUnpinnedBaseImages(t *testing.T) {
	const digest = "@sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	tests := []struct {
		name    string
		content string
		want    []unpinnedBaseImage
	}{
		{
			name:    "tag",
			content: "FROM nginx:1.27\n",
			want:    []unpinnedBaseImage{{Line: 0, Image: "nginx:1.27", Resolved: "nginx:1.27"}},
		},
		{
			name:    "digest",
			content: "FROM nginx" + digest + "\n",
		},
		{
			name:    "arg default resolved",
			content: "ARG NGINX_VERSION=1.27\nFROM nginx:${NGINX_VERSION}\n",
			want:    []unpinnedBaseImage{{Line: 1, Image: "nginx:${NGINX_VERSION}", Resolved: "nginx:1.27"}},
		},
		{
			name:    "bare $VAR and quoted default",
			content: "ARG BASE=\"node:22\"\nFROM $BASE\n",
			want:    []unpinnedBaseImage{{Line: 1, Image: "$BASE", Resolved: "node:22"}},
		},
		{
			name:    "multiple args on one line",
			content: "ARG IMAGE=node TAG=22\nFROM ${IMAGE}:${TAG}\n",
			want:    []unpinnedBaseImage{{Line: 1, Image: "${IMAGE}:${TAG}", Resolved: "node:22"}},
		},
		{
			name:    "arg default with digest passes",
			content: "ARG BASE=node:22" + digest + "\nFROM ${BASE}\n",
		},
		{
			name:    "arg without default skipped",
			content: "ARG BASE\nFROM ${BASE}\n",
		},
		{
			name:    "undeclared arg skipped",
			content: "FROM ${BASE}\n",
		},
		{
			name:    "colon-dash default",
			content: "ARG TAG\nFROM node:${TAG:-22}\n",
			want:    []unpinnedBaseImage{{Line: 1, Image: "node:${TAG:-22}", Resolved: "node:22"}},
		},
		{
			name:    "arg declared inside a stage is not visible to later FROM",
			content: "FROM golang" + digest + " AS build\nARG TAG=1.27\nFROM nginx:${TAG}\n",
		},
		{
			name: "build stage reference skipped",
			content: "FROM golang:1.23 AS build\n" +
				"RUN go build\n" +
				"FROM build AS test\n" +
				"FROM scratch\n",
			want: []unpinnedBaseImage{{Line: 0, Image: "golang:1.23", Resolved: "golang:1.23"}},
		},
		{
			name:    "stage named like its own image still reported",
			content: "FROM node:22 AS node\n",
			want:    []unpinnedBaseImage{{Line: 0, Image: "node:22", Resolved: "node:22"}},
		},
		{
			name:    "platform flag and lowercase from",
			content: "from --platform=$BUILDPLATFORM node:22 as build\n",
			want:    []unpinnedBaseImage{{Line: 0, Image: "node:22", Resolved: "node:22"}},
		},
		{
			name:    "arg default referencing earlier arg with digest passes",
			content: "ARG DIGEST=sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\nARG BASE=node@${DIGEST}\nFROM ${BASE}\n",
		},
		{
			name:    "arg default referencing arg without default skipped",
			content: "ARG TAG\nARG BASE=node:${TAG}\nFROM ${BASE}\n",
		},
		{
			name:    "dash default without colon",
			content: "FROM node:${TAG-22}\n",
			want:    []unpinnedBaseImage{{Line: 0, Image: "node:${TAG-22}", Resolved: "node:22"}},
		},
		{
			name:    "plus alternative",
			content: "ARG SUFFIX=1\nFROM node:22${SUFFIX:+-alpine}\n",
			want:    []unpinnedBaseImage{{Line: 1, Image: "node:22${SUFFIX:+-alpine}", Resolved: "node:22-alpine"}},
		},
		{
			name:    "line continuation",
			content: "FROM \\\n    nginx:1.27 \\\n    AS web\n",
			want:    []unpinnedBaseImage{{Line: 0, Image: "nginx:1.27", Resolved: "nginx:1.27"}},
		},
		{
			name:    "line numbers after a continuation",
			content: "ARG A=1 \\\n    B=2\nFROM node:${B}\n",
			want:    []unpinnedBaseImage{{Line: 2, Image: "node:${B}", Resolved: "node:2"}},
		},
		{
			name:    "allow comment on line above",
			content: "ARG V=1.27\n# git-cascade:allow\nFROM nginx:${V}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findUnpinnedBaseImages(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
