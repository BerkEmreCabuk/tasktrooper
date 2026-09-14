package graph_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
)

type GraphSuite struct {
	suite.Suite
}

type mapLookup map[string]graph.SymbolRef

func (m mapLookup) Lookup(filePath, symbolName string) (graph.SymbolRef, bool) {
	ref, ok := m[filePath+":"+symbolName]
	return ref, ok
}

func (s *GraphSuite) TestBuildGoCallGraph() {
	src := []byte(`package svc

func Alpha() {
	Beta()
	Gamma()
}

func Beta() {}

func Gamma() {}
`)
	edges, err := graph.BuildGoCallGraph("svc.go", src)
	s.Require().NoError(err)
	s.Require().NotEmpty(edges)

	callees := make(map[string]struct{})
	for _, e := range edges {
		if e.From.SymbolName == "Alpha" && e.Kind == graph.EdgeCall {
			callees[e.To.SymbolName] = struct{}{}
		}
	}
	s.Contains(callees, "Beta")
	s.Contains(callees, "Gamma")
}

func (s *GraphSuite) TestExtractGoImports() {
	src := []byte(`package main

import (
	"fmt"
	"os"
)
`)
	edges, err := graph.ExtractGoImports("main.go", src)
	s.Require().NoError(err)
	s.Require().Len(edges, 2)
	s.Equal(graph.EdgeImport, edges[0].Kind)
	s.Equal("fmt", edges[0].To.SymbolName)
}

func (s *GraphSuite) TestExtractTSImportsAndRequire() {
	src := []byte(`import { readFile } from 'fs/promises';
const path = require('./util');
`)
	edges := graph.ExtractImports("index.ts", src)
	s.Require().Len(edges, 2)
	s.Equal("fs/promises", edges[0].To.SymbolName)
	s.Equal("./util", edges[1].To.SymbolName)
}

func (s *GraphSuite) TestExpandContextBFS() {
	g := graph.NewDependencyGraph()
	alpha := graph.SymbolRef{FilePath: "svc.go", SymbolName: "Alpha", Kind: "function"}
	beta := graph.SymbolRef{FilePath: "svc.go", SymbolName: "Beta", Kind: "function"}
	gamma := graph.SymbolRef{FilePath: "svc.go", SymbolName: "Gamma", Kind: "function"}

	g.AddEdge(graph.Edge{From: alpha, To: beta, Kind: graph.EdgeCall})
	g.AddEdge(graph.Edge{From: beta, To: gamma, Kind: graph.EdgeCall})

	lookup := mapLookup{
		"svc.go:Beta":  beta,
		"svc.go:Gamma": gamma,
	}

	refs, keys := graph.ExpandContext(g, lookup, alpha, "svc.go", 2, 8)
	s.Require().NotEmpty(refs)
	s.Equal(alpha.Key(), refs[0].Key())
	s.Contains(keys, alpha.Key())
	s.Contains(keys, beta.Key())
	s.Contains(keys, gamma.Key())
}

func TestGraphSuite(t *testing.T) {
	suite.Run(t, new(GraphSuite))
}
