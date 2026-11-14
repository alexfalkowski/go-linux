//go:build linux

// Package test provides shared Linux test fixtures and bounded synchronization.
// Helpers own setup/cleanup and compact recurring result checks. Expected
// behavior and scenario-specific assertions remain beside package test cases.
package test
