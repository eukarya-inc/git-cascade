package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
)

const testDigest = "@sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

func TestVersionTaggedDigestPattern(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"nginx:1.27.3" + testDigest, true},
		{"nginx:1.27.3-alpine" + testDigest, true},
		{"node:22.11.0-bookworm-slim" + testDigest, true},
		{"gcr.io/distroless/static:1.2.3" + testDigest, true},
		{"localhost:5000/team/app:10.0.1" + testDigest, true},
		{"nginx:1.27.3.1" + testDigest, true},
		{"eclipse-temurin:21.0.5_11-jre" + testDigest, true},
		{"traefik:v3.2.0" + testDigest, true},
		{"python:3.13.0rc2-slim" + testDigest, true},
		{"debian:12.8-slim" + testDigest, false},
		{"gcr.io/distroless/static-debian12:nonroot" + testDigest, false},
		{"nginx:1.27" + testDigest, false},
		{"nginx:1" + testDigest, false},
		{"nginx:latest" + testDigest, false},
		{"nginx:stable-1.27.3" + testDigest, false},
		{"nginx" + testDigest, false},
		{"nginx:1.27.3", false},
		{"localhost:5000/app" + testDigest, false},
		{"localhost:5000/app:latest" + testDigest, false},
	}
	for _, tt := range tests {
		if got := versionTaggedDigest.MatchString(tt.ref); got != tt.want {
			t.Errorf("versionTaggedDigest.MatchString(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}

func TestFindUnversionedFinalImage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *unversionedFinalImage
	}{
		{
			name:    "version tag and digest",
			content: "FROM nginx:1.27.3" + testDigest + "\n",
		},
		{
			name:    "digest only",
			content: "FROM nginx" + testDigest + "\n",
			want:    &unversionedFinalImage{Line: 0, Image: "nginx" + testDigest, Resolved: "nginx" + testDigest},
		},
		{
			name:    "version tag without digest",
			content: "FROM nginx:1.27.3\n",
			want:    &unversionedFinalImage{Line: 0, Image: "nginx:1.27.3", Resolved: "nginx:1.27.3"},
		},
		{
			name: "only the final stage is checked",
			content: "FROM golang:1.23 AS build\n" +
				"RUN go build\n" +
				"FROM nginx:1.27.3" + testDigest + "\n",
		},
		{
			name: "final stage built on a bad image",
			content: "FROM golang:1.23.4" + testDigest + " AS build\n" +
				"FROM nginx:1.27" + testDigest + "\n",
			want: &unversionedFinalImage{Line: 1, Image: "nginx:1.27" + testDigest, Resolved: "nginx:1.27" + testDigest},
		},
		{
			name: "final stage from earlier stage follows the chain",
			content: "FROM node:22" + testDigest + " AS base\n" +
				"FROM base AS deps\n" +
				"FROM deps\n",
			want: &unversionedFinalImage{Line: 0, Image: "node:22" + testDigest, Resolved: "node:22" + testDigest},
		},
		{
			name: "chain to a good image passes",
			content: "FROM node:22.11.0" + testDigest + " AS base\n" +
				"FROM base\n",
		},
		{
			name:    "arg resolved",
			content: "ARG NGINX_VERSION=1.27\nFROM nginx:${NGINX_VERSION}\n",
			want:    &unversionedFinalImage{Line: 1, Image: "nginx:${NGINX_VERSION}", Resolved: "nginx:1.27"},
		},
		{
			name:    "arg resolved to good form passes",
			content: "ARG V=1.27.3\nFROM nginx:${V}" + testDigest + "\n",
		},
		{
			name:    "arg without default skipped",
			content: "ARG BASE\nFROM ${BASE}\n",
		},
		{
			name:    "scratch final stage skipped",
			content: "FROM golang:1.23 AS build\nFROM scratch\n",
		},
		{
			name:    "no FROM",
			content: "# empty\n",
		},
		{
			name:    "allow comment on final FROM",
			content: "FROM node:22" + testDigest + " AS base\nFROM base # git-cascade:allow\n",
		},
		{
			name: "allow comment on the root of a chain",
			content: "FROM node:22" + testDigest + " AS base # git-cascade:allow\n" +
				"RUN true\n" +
				"FROM base\n",
		},
		{
			name: "allow on the line above a stage-reference final FROM does not suppress",
			content: "FROM node:22" + testDigest + " AS base\n" +
				"FROM golang:1.23 AS build # git-cascade:allow\n" +
				"FROM base\n",
			want: &unversionedFinalImage{Line: 0, Image: "node:22" + testDigest, Resolved: "node:22" + testDigest},
		},
		{
			name:    "allow on the line above the FROM naming the image",
			content: "# git-cascade:allow\nFROM nginx:1.27\n",
		},
		{
			name: "stage names are case-insensitive",
			content: "FROM node:22.11.0" + testDigest + " AS Base\n" +
				"FROM base\n",
		},
		{
			name: "FROM inside a heredoc is not the final stage",
			content: "FROM nginx:1.27\n" +
				"RUN <<EOF cat > /tmp/Dockerfile\n" +
				"FROM node:22.11.0" + testDigest + "\n" +
				"EOF\n",
			want: &unversionedFinalImage{Line: 0, Image: "nginx:1.27", Resolved: "nginx:1.27"},
		},
		{
			name:    "allow comment on the FROM naming the image",
			content: "FROM nginx:1.27 # git-cascade:allow\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findUnversionedFinalImage(tt.content)
			switch {
			case got == nil && tt.want == nil:
			case got == nil || tt.want == nil || *got != *tt.want:
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDockerfileVersionTagChecker(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "Dockerfile", []byte("FROM nginx:1.27.3"+testDigest+"\n"))
	fake.setFile("org", "repo", "web/Dockerfile", []byte("FROM golang:1.23 AS build\nFROM nginx:1.27"+testDigest+"\n"))
	_, client := fake.serve(t)

	c := &dockerfileVersionTagChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("dockerfile-version-tag"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Fatalf("expected fail, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "1 final-stage image(s)") || !strings.Contains(result.Message, "web/Dockerfile:2: FROM nginx:1.27@sha256:") {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestDockerfileVersionTagChecker_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "Dockerfile", []byte("FROM nginx:1.27.3-alpine"+testDigest+"\n"))
	_, client := fake.serve(t)

	c := &dockerfileVersionTagChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("dockerfile-version-tag"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestDockerfileVersionTagChecker_NoDockerfile(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "main.go", []byte("package main\n"))
	_, client := fake.serve(t)

	c := &dockerfileVersionTagChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("dockerfile-version-tag"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip, got %s: %s", result.Status, result.Message)
	}
}
