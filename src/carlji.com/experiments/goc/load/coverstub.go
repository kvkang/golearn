package load

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
)

func init() {
	mux := http.NewServeMux()
	mux.HandleFunc("/stcover/report", CoverReport)
	mux.HandleFunc("/stcover/coverage", Coverage)
	ln, err := net.Listen("tcp", ":50102")
	if err != nil {
		log.Fatalln("net listen failed", err)
	}
	log.Fatal(http.Serve(ln, mux))
}

// CoverReport responses the detailed cover profile
func CoverReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	coverReport(w)
}

// Coverage responses the currently test coverage
func Coverage(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf("%v", coverage()*100)))
}

// CoverBlock records the coverage data for a single basic block.
// NOTE: This struct is internal to the testing infrastructure and may change.
// It is not covered (yet) by the Go 1 compatibility guidelines.
type CoverBlock struct {
	Line0 uint32
	Col0  uint16
	Line1 uint32
	Col1  uint16
	Stmts uint16
}

var cover Cover

// Cover records information about test coverage checking.
// NOTE: This struct is internal to the testing infrastructure and may change.
// It is not covered (yet) by the Go 1 compatibility guidelines.
type Cover struct {
	Mode            string
	Counters        map[string][]uint32
	Blocks          map[string][]CoverBlock
	CoveredPackages string
}

// Coverage reports the current code coverage as a fraction in the range [0, 1].
// If coverage is not enabled, Coverage returns 0.
//
// When running a large set of sequential test cases, checking Coverage after each one
// can be useful for identifying which test cases exercise new code paths.
// It is not a replacement for the reports generated by 'go test -cover' and
// 'go tool cover'.
func coverage() float64 {
	var n, d int64
	for _, counters := range cover.Counters {
		for i := range counters {
			if atomic.LoadUint32(&counters[i]) > 0 {
				n++
			}
			d++
		}
	}
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// mustBeNil checks the error and, if present, reports it and exits.
func mustBeNil(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "testing: %s\n", err)
		os.Exit(2)
	}
}

// coverReport reports the coverage percentage and writes a coverage profile if requested.
func coverReport(w io.Writer) {
	var active, total int64
	var count uint32
	for name, counts := range cover.Counters {
		blocks := cover.Blocks[name]
		for i := range counts {
			stmts := int64(blocks[i].Stmts)
			total += stmts
			count = atomic.LoadUint32(&counts[i]) // For -mode=atomic.
			if count > 0 {
				active += stmts
			}

			_, err := fmt.Fprintf(w, "%s:%d.%d,%d.%d %d %d\n", name,
				blocks[i].Line0, blocks[i].Col0,
				blocks[i].Line1, blocks[i].Col1,
				stmts,
				count)
			mustBeNil(err)
		}
	}
	if total == 0 {
		total = 1
	}
	fmt.Printf("coverage: %.1f%% of statements%s\n", 100*float64(active)/float64(total), cover.CoveredPackages)
}

var coverMainImpl = template.Must(template.New("main").Parse(`
package main

import (
{{if not .TestMain}}
	"os"
{{end}}
	"testing"
	"testing/internal/testdeps"

{{if .ImportTest}}
	{{if .NeedTest}}_test{{else}}_{{end}} {{.Package.ImportPath | printf "%q"}}
{{end}}
{{if .ImportXtest}}
	{{if .NeedXtest}}_xtest{{else}}_{{end}} {{.Package.ImportPath | printf "%s_test" | printf "%q"}}
{{end}}
{{if .Cover}}
{{range $i, $p := .Cover.Vars}}
	_cover{{$i}} {{$p.Package.ImportPath | printf "%q"}}
{{end}}
{{end}}
)

var tests = []testing.InternalTest{
{{range .Tests}}
	{"{{.Name}}", {{.Package}}.{{.Name}}},
{{end}}
}

var benchmarks = []testing.InternalBenchmark{
{{range .Benchmarks}}
	{"{{.Name}}", {{.Package}}.{{.Name}}},
{{end}}
}

var examples = []testing.InternalExample{
{{range .Examples}}
	{"{{.Name}}", {{.Package}}.{{.Name}}, {{.Output | printf "%q"}}, {{.Unordered}}},
{{end}}
}

func init() {
	testdeps.ImportPath = {{.ImportPath | printf "%q"}}
}

{{if .Cover}}

// Only updated by init functions, so no need for atomicity.
var (
	coverCounters = make(map[string][]uint32)
	coverBlocks = make(map[string][]testing.CoverBlock)
)

func init() {
	{{range $i, $p := .Cover.Vars}}
	{{range $file, $cover := $p.Vars}}
	coverRegisterFile({{printf "%q" $cover.File}}, _cover{{$i}}.{{$cover.Var}}.Count[:], _cover{{$i}}.{{$cover.Var}}.Pos[:], _cover{{$i}}.{{$cover.Var}}.NumStmt[:])
	{{end}}
	{{end}}
}

func coverRegisterFile(fileName string, counter []uint32, pos []uint32, numStmts []uint16) {
	if 3*len(counter) != len(pos) || len(counter) != len(numStmts) {
		panic("coverage: mismatched sizes")
	}
	if coverCounters[fileName] != nil {
		// Already registered.
		return
	}
	coverCounters[fileName] = counter
	block := make([]testing.CoverBlock, len(counter))
	for i := range counter {
		block[i] = testing.CoverBlock{
			Line0: pos[3*i+0],
			Col0: uint16(pos[3*i+2]),
			Line1: pos[3*i+1],
			Col1: uint16(pos[3*i+2]>>16),
			Stmts: numStmts[i],
		}
	}
	coverBlocks[fileName] = block
}
{{end}}

func main() {
{{if .Cover}}
	testing.RegisterCover(testing.Cover{
		Mode: {{printf "%q" .Cover.Mode}},
		Counters: coverCounters,
		Blocks: coverBlocks,
		CoveredPackages: {{printf "%q" .Covered}},
	})
{{end}}
	m := testing.MainStart(testdeps.TestDeps{}, tests, benchmarks, examples)
{{with .TestMain}}
	{{.Package}}.{{.Name}}(m)
{{else}}
	os.Exit(m.Run())
{{end}}
}

`))