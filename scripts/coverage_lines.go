// coverage_lines reports executable Go statement lines from the standard Go cover profile.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type position struct{ line, column int }
type block struct {
	start, end position
	hit        bool
}
type score struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

func before(a, b position) bool {
	return a.line < b.line || a.line == b.line && a.column < b.column
}

func profile(path string) (map[string][]block, error) {
	input, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	const prefix = "github.com/hackchange/avito-rag/"
	files := map[string][]block{}
	scanner := bufio.NewScanner(input)
	scanner.Scan()
	for scanner.Scan() {
		name, span, ok := strings.Cut(scanner.Text(), ":")
		if !ok || !strings.HasPrefix(name, prefix) {
			return nil, fmt.Errorf("bad profile entry: %q", scanner.Text())
		}
		var b block
		var statements, hits int
		if _, err := fmt.Sscanf(span, "%d.%d,%d.%d %d %d", &b.start.line, &b.start.column, &b.end.line, &b.end.column, &statements, &hits); err != nil {
			return nil, err
		}
		if statements > 0 {
			b.hit = hits > 0
			file := strings.TrimPrefix(name, prefix)
			files[file] = append(files[file], b)
		}
	}
	return files, scanner.Err()
}

func sourceLines(name string, blocks []block) (score, error) {
	fset := token.NewFileSet()
	source, err := parser.ParseFile(fset, filepath.FromSlash(name), nil, 0)
	if err != nil {
		return score{}, err
	}
	lines := map[int]bool{}
	ast.Inspect(source, func(node ast.Node) bool {
		if _, isStatement := node.(ast.Stmt); !isStatement {
			return true
		}
		switch node.(type) {
		case *ast.BlockStmt, *ast.EmptyStmt, *ast.LabeledStmt, *ast.CaseClause, *ast.CommClause:
			return true
		}
		p := fset.Position(node.Pos())
		pos := position{p.Line, p.Column}
		for _, span := range blocks {
			if !before(pos, span.start) && before(pos, span.end) {
				lines[p.Line] = lines[p.Line] || span.hit
			}
		}
		return true
	})
	covered := 0
	for _, hit := range lines {
		if hit {
			covered++
		}
	}
	return score{Covered: covered, Total: len(lines)}, nil
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run ./scripts/coverage_lines.go GO_COVER_PROFILE")
	}
	files, err := profile(os.Args[1])
	if err != nil {
		return err
	}
	output := struct {
		Total score            `json:"total"`
		Files map[string]score `json:"files"`
	}{Files: map[string]score{}}
	for name, blocks := range files {
		m, err := sourceLines(name, blocks)
		if err != nil {
			return err
		}
		output.Files[name] = m
		output.Total.Covered += m.Covered
		output.Total.Total += m.Total
	}
	if output.Total.Total == 0 {
		return fmt.Errorf("no executable lines were measured")
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
