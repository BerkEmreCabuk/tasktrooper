package graph_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
)

type LangGraphSuite struct {
	suite.Suite
}

func TestLangGraphSuite(t *testing.T) {
	suite.Run(t, new(LangGraphSuite))
}

func importTargets(edges []graph.Edge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		if e.Kind == graph.EdgeImport {
			out = append(out, e.To.SymbolName)
		}
	}
	return out
}

func callNames(edges []graph.Edge) map[string]string {
	out := make(map[string]string, len(edges))
	for _, e := range edges {
		if e.Kind == graph.EdgeCall {
			out[e.To.SymbolName] = e.To.Kind
		}
	}
	return out
}

func (s *LangGraphSuite) TestSwiftImportsAndCalls() {
	src := []byte(`import SwiftUI
import Combine

final class Store {
    func fetch() {
        api.load()
        refresh()
    }
}
`)
	s.ElementsMatch([]string{"SwiftUI", "Combine"}, importTargets(graph.ExtractImports("Store.swift", src)))

	calls := callNames(graph.BuildCallGraphInRange("Store.swift", "Store.fetch", src, 5, 8))
	s.Equal("method", calls["load"])
	s.Equal("function", calls["refresh"])
}

func (s *LangGraphSuite) TestKotlinImportsAndCalls() {
	src := []byte(`package com.example

import kotlinx.coroutines.flow.Flow

class Repo(private val api: Api) {
    suspend fun save(item: Item) {
        api.put(item)
        validate(item)
    }
}
`)
	s.Equal([]string{"kotlinx.coroutines.flow.Flow"}, importTargets(graph.ExtractImports("Repo.kt", src)))

	calls := callNames(graph.BuildCallGraphInRange("Repo.kt", "Repo.save", src, 6, 9))
	s.Equal("method", calls["put"])
	s.Equal("function", calls["validate"])
}

func (s *LangGraphSuite) TestJavaImportsAndCalls() {
	src := []byte(`package com.example;

import java.util.List;
import static org.junit.Assert.assertTrue;

public class Greeter {
    public String hello() {
        String value = formatter.format("hi");
        return build(value);
    }
}
`)
	s.ElementsMatch(
		[]string{"java.util.List", "org.junit.Assert.assertTrue"},
		importTargets(graph.ExtractImports("Greeter.java", src)),
	)

	calls := callNames(graph.BuildCallGraphInRange("Greeter.java", "Greeter.hello", src, 7, 10))
	s.Equal("method", calls["format"])
	s.Equal("function", calls["build"])
}

func (s *LangGraphSuite) TestPythonImportsAndCalls() {
	src := []byte(`import os
from pathlib import Path


def run():
    load()
    os.getenv("HOME")
`)
	s.ElementsMatch([]string{"os", "pathlib"}, importTargets(graph.ExtractImports("run.py", src)))

	calls := callNames(graph.BuildCallGraphInRange("run.py", "run", src, 5, 7))
	s.Equal("function", calls["load"])
	s.Equal("method", calls["getenv"])
}

func (s *LangGraphSuite) TestTSXComponentCallsAreScopedByRange() {
	src := []byte(`import { useState } from "react";

export function Panel() {
  const [open, setOpen] = useState(false);
  return <button onClick={() => toggle(open)}>x</button>;
}

export class Widget {
  render() {
    return draw();
  }
}
`)
	s.Equal([]string{"react"}, importTargets(graph.ExtractImports("Panel.tsx", src)))

	panelCalls := callNames(graph.BuildCallGraphInRange("Panel.tsx", "Panel", src, 3, 6))
	s.Equal("function", panelCalls["useState"])
	s.Equal("function", panelCalls["toggle"])
	s.NotContains(panelCalls, "draw", "calls leaked in from another symbol")

	widgetCalls := callNames(graph.BuildCallGraphInRange("Panel.tsx", "Widget.render", src, 9, 11))
	s.Equal("function", widgetCalls["draw"])
}

func (s *LangGraphSuite) TestUnsupportedLanguageProducesNoEdges() {
	s.False(graph.HasGrammarGraph("notes.md"))
	s.Empty(graph.ExtractImports("notes.md", []byte("# hello\n")))
}
