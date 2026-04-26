package local

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"strings"

	"github.com/fzipp/gocyclo"
	"github.com/uudashr/gocognit"
)

// Thresholds for the Go fallback heuristic. Functions exceeding these
// values count as smells.
const (
	cycloWarn       = 10
	cognitWarn      = 15
	longFnLineCount = 60
)

// ScoreGoFile parses one Go file and produces an approximate health score.
// Algorithm: collect cyclomatic + cognitive complexity per function,
// flag functions over thresholds, derive a 0..10 score from the worst fn
// (clamped). This is a coarse approximation — never authoritative.
func ScoreGoFile(path string) (*Score, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, ".go") {
		return &Score{Path: path, Health: 10, Backend: "go-fallback"}, nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	cycloStats := gocyclo.AnalyzeASTFile(f, fset, nil)
	cognitStats := gocognit.ComplexityStats(f, fset, nil)

	cogIndex := map[string]gocognit.Stat{}
	for _, s := range cognitStats {
		cogIndex[s.PkgName+"."+s.FuncName] = s
	}

	var smells []Smell
	worst := 0

	for _, st := range cycloStats {
		key := st.PkgName + "." + st.FuncName
		cog := cogIndex[key]

		if st.Complexity > worst {
			worst = st.Complexity
		}
		if cog.Complexity > worst {
			worst = cog.Complexity
		}

		if st.Complexity >= cycloWarn {
			smells = append(smells, Smell{
				Function: st.FuncName,
				Line:     st.Pos.Line,
				Kind:     "cyclomatic",
				Value:    st.Complexity,
				Message:  "cyclomatic complexity high",
			})
		}
		if cog.Complexity >= cognitWarn {
			smells = append(smells, Smell{
				Function: st.FuncName,
				Line:     st.Pos.Line,
				Kind:     "cognitive",
				Value:    cog.Complexity,
				Message:  "cognitive complexity high",
			})
		}

		// long-function check: end - start of function body
		if fnLines := lineSpan(f, fset, st.FuncName); fnLines >= longFnLineCount {
			smells = append(smells, Smell{
				Function: st.FuncName,
				Line:     st.Pos.Line,
				Kind:     "long_function",
				Value:    fnLines,
				Message:  "function spans many lines",
			})
		}
	}

	return &Score{
		Path:    path,
		Health:  scoreFromWorst(worst),
		Backend: "go-fallback",
		Smells:  smells,
	}, nil
}

// scoreFromWorst maps the worst-function complexity into a 0..10 score.
// Piecewise-linear: <=5 → 10; 5..10 → 10..8; 10..20 → 8..5; 20..30 → 5..2;
// >=30 → 1. Approximation only — never authoritative.
func scoreFromWorst(worst int) float64 {
	w := float64(worst)
	var score float64
	switch {
	case worst <= 5:
		score = 10
	case worst <= 10:
		score = 10 - 2*(w-5)/5
	case worst <= 20:
		score = 8 - 3*(w-10)/10
	case worst <= 30:
		score = 5 - 3*(w-20)/10
	default:
		score = 1
	}
	if score < 1 {
		score = 1
	}
	if score > 10 {
		score = 10
	}
	return math.Round(score*100) / 100
}

func lineSpan(f *ast.File, fset *token.FileSet, name string) int {
	var span int
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Body == nil {
			return true
		}
		start := fset.Position(fn.Pos()).Line
		end := fset.Position(fn.End()).Line
		span = end - start
		return false
	})
	return span
}
