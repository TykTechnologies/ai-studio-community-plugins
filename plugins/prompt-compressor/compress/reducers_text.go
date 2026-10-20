package compress

import (
	"fmt"
	"regexp"
	"strings"
)

// Level-2 reducers for logs and listings. Each keeps every line that
// mentions an error, failure, panic or exception (keepErrors) and never
// changes a line it keeps: ids, paths, numbers and codes stay verbatim (R3).

// planFromDrops makes a plan from a drop mask, with no notes.
func planFromDrops(drop []bool) cutPlan {
	return cutPlan{drop: drop, failures: -1}
}

// terraform plan: Refreshing / Reading lines and "(known after apply)"
// attributes of created resources go; every resource header, every changed
// attribute and the Plan: summary stay.
var (
	tfActions  = regexp.MustCompile(`^(Terraform will perform the following actions:|Plan: \d+ to add, \d+ to change, \d+ to destroy)`)
	tfRefresh  = regexp.MustCompile(`: (Refreshing state\.\.\.|Reading\.\.\.|Read complete after \S+)( \[id=\S+\])?$`)
	tfUnknown  = regexp.MustCompile(`^\s+\+ \S+\s+= \(known after apply\)$`)
	tfResource = regexp.MustCompile(`^\s+# \S+ (will be|must be)\b`)
)

var terraformReducer = reducer{
	name: "terraform plan", version: 1, keepErrors: true,
	detect: func(lines []string) bool {
		return countMatches(lines, tfActions, tfResource) > 0 && countMatches(lines, tfRefresh, tfUnknown) > 0
	},
	plan: func(lines []string) cutPlan {
		drop := make([]bool, len(lines))
		for i, l := range lines {
			drop[i] = tfRefresh.MatchString(l) || tfUnknown.MatchString(l)
		}
		return planFromDrops(drop)
	},
}

// kubectl: in a get table, healthy rows (Running or Completed, all
// containers ready, no restarts) go; the header and every other row stay.
// In describe output, label, annotation, toleration, volume and mount
// details and Normal events go; Warning events and everything else stay.
var (
	kubeHeader    = regexp.MustCompile(`^(NAMESPACE\s+)?NAME\s+READY\s+STATUS\s+RESTARTS\s+AGE\b`)
	kubeDescribe  = regexp.MustCompile(`^(Name|Namespace):\s+\S`)
	kubeEvents    = regexp.MustCompile(`^Events:`)
	kubeTopKey    = regexp.MustCompile(`^[A-Z][A-Za-z -]*:`)
	kubeNoisyTop  = regexp.MustCompile(`^(Labels|Annotations|Tolerations|Volumes|Node-Selectors):`)
	kubeNoisySub  = regexp.MustCompile(`^\s+(Mounts|Environment):\s*$`)
	kubeNormalEvt = regexp.MustCompile(`^\s+Normal\s+\S`)
	kubeReady     = regexp.MustCompile(`^(\d+)/(\d+)$`)
)

var kubectlReducer = reducer{
	name: "kubectl", version: 1, keepErrors: true,
	detect: func(lines []string) bool {
		return countMatches(lines, kubeHeader) > 0 || (countMatches(lines, kubeDescribe) >= 2 && countMatches(lines, kubeTopKey) >= 5)
	},
	plan: func(lines []string) cutPlan {
		if countMatches(lines, kubeHeader) > 0 {
			return kubectlGet(lines)
		}
		return kubectlDescribe(lines)
	},
}

func kubectlGet(lines []string) cutPlan {
	drop := make([]bool, len(lines))
	notes := map[int]note{}
	ready, status, restarts := -1, -1, -1
	runStart, runLen := -1, 0
	flush := func() {
		if runLen > 0 {
			text := fmt.Sprintf("[tyk-compressed: %d healthy rows]", runLen)
			if runLen == 1 {
				text = "[tyk-compressed: 1 healthy row]"
			}
			notes[runStart] = note{n: runLen, text: text}
		}
		runStart, runLen = -1, 0
	}
	for i, l := range lines {
		f := strings.Fields(l)
		if kubeHeader.MatchString(l) {
			flush()
			ready, status, restarts = index(f, "READY"), index(f, "STATUS"), index(f, "RESTARTS")
			continue
		}
		healthy := ready >= 0 && len(f) > restarts && restarts > status && status > ready
		if healthy {
			m := kubeReady.FindStringSubmatch(f[ready])
			st := f[status]
			healthy = m != nil && (st == "Running" && m[1] == m[2] || st == "Completed" || st == "Succeeded") && f[restarts] == "0"
		}
		if !healthy {
			flush()
			continue
		}
		drop[i] = true
		if runStart < 0 {
			runStart = i
		}
		runLen++
	}
	flush()
	return cutPlan{drop: drop, notes: notes, failures: -1}
}

func index(fields []string, name string) int {
	for i, f := range fields {
		if f == name {
			return i
		}
	}
	return -1
}

func kubectlDescribe(lines []string) cutPlan {
	drop := make([]bool, len(lines))
	noisyTop, noisySub, events := false, -1, false
	for i, l := range lines {
		indent := len(l) - len(strings.TrimLeft(l, " "))
		switch {
		case kubeEvents.MatchString(l):
			noisyTop, noisySub, events = false, -1, true
			continue
		case kubeTopKey.MatchString(l) && indent == 0:
			noisyTop, noisySub, events = kubeNoisyTop.MatchString(l), -1, false
			// The key's own line keeps its first value.
			continue
		case kubeNoisySub.MatchString(l):
			noisySub = indent
			continue
		}
		switch {
		case events:
			drop[i] = kubeNormalEvt.MatchString(l)
		case noisyTop:
			drop[i] = indent > 0
		case noisySub >= 0 && indent > noisySub:
			drop[i] = true
		default:
			noisySub = -1
		}
	}
	return planFromDrops(drop)
}

// git diff: context lines further than diffContext from a change go; file
// headers, hunk headers and every changed line stay.
const diffContext = 3

var (
	diffFile = regexp.MustCompile(`^diff --git `)
	diffHunk = regexp.MustCompile(`^@@ -\d+(,\d+)? \+\d+(,\d+)? @@`)
)

var gitDiffReducer = reducer{
	name: "git diff", version: 1, keepErrors: true,
	detect: func(lines []string) bool {
		return countMatches(lines, diffFile) > 0 && countMatches(lines, diffHunk) > 0
	},
	plan: func(lines []string) cutPlan {
		drop := make([]bool, len(lines))
		inHunk := false
		var context []int // indexes of the current hunk's context lines
		var changes []int
		finish := func() {
			for _, c := range context {
				near := false
				for _, ch := range changes {
					if d := c - ch; d <= diffContext && d >= -diffContext {
						near = true
						break
					}
				}
				drop[c] = !near
			}
			context, changes = nil, nil
		}
		for i, l := range lines {
			switch {
			case diffFile.MatchString(l), diffHunk.MatchString(l):
				finish()
				inHunk = diffHunk.MatchString(l)
			case !inHunk:
			case strings.HasPrefix(l, " ") || l == "":
				context = append(context, i)
			case strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-"):
				changes = append(changes, i)
			default:
				finish()
				inHunk = false
			}
		}
		finish()
		return planFromDrops(drop)
	},
}

// grep / ripgrep: at most grepPerFile matches per file stay; the rest of a
// file's matches go with a count.
const grepPerFile = 5

var (
	grepLine    = regexp.MustCompile(`^([^\s:][^:]*):(\d+)[:-]`)
	grepHeading = regexp.MustCompile(`^\d+[:-]`)
	grepPath    = regexp.MustCompile(`^[^\s:]+\.[A-Za-z0-9]+$|^[^\s:]+/[^\s:]+$`)
)

var grepReducer = reducer{
	name: "grep", version: 1, keepErrors: true,
	detect: func(lines []string) bool {
		n, matched := 0, 0
		for _, l := range lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			n++
			if grepLine.MatchString(l) || grepHeading.MatchString(l) || grepPath.MatchString(l) {
				matched++
			}
		}
		return n >= minReducerLines && matched*10 >= n*9 && countMatches(lines, grepLine, grepHeading) >= minReducerLines
	},
	plan: func(lines []string) cutPlan {
		drop := make([]bool, len(lines))
		perFile := map[string]int{}
		fileOf := make([]string, len(lines))
		heading := ""
		for i, l := range lines {
			file := ""
			switch {
			case grepLine.MatchString(l):
				file = grepLine.FindStringSubmatch(l)[1]
			case grepHeading.MatchString(l) && heading != "":
				file = heading
			case grepPath.MatchString(l):
				heading = l
				continue
			default:
				if strings.TrimSpace(l) == "" {
					heading = ""
				}
				continue
			}
			fileOf[i] = file
			perFile[file]++
			drop[i] = perFile[file] > grepPerFile
		}
		// A run of one file's extra matches says which file and how many.
		notes := map[int]note{}
		for i := 0; i < len(lines); {
			if !drop[i] {
				i++
				continue
			}
			j, same := i, true
			for j < len(lines) && drop[j] {
				same = same && fileOf[j] == fileOf[i]
				j++
			}
			if same {
				notes[i] = note{n: j - i, text: fmt.Sprintf("[tyk-compressed: %d more matches in %s]", j-i, fileOf[i])}
			}
			i = j
		}
		return cutPlan{drop: drop, notes: notes, failures: -1}
	},
}

// Directory listings (find, tree, ls -l): entries deeper than listDepth,
// entries under dependency and build directories, and entries past
// listPerDir in one directory go.
const (
	listDepth  = 4
	listPerDir = 30
)

var (
	listLong  = regexp.MustCompile(`^[-dlcbps][rwxsStT-]{9}[.@+]?\s+\d+\s`)
	listTree  = regexp.MustCompile(`^((?:│   |    )*)(├── |└── )`)
	listFind  = regexp.MustCompile(`^\.?/?[^\s:]+$`)
	listNoise = regexp.MustCompile(`/(node_modules|\.git|vendor|dist|build|target|__pycache__|\.venv|\.next|\.cache)/`)
)

var listingReducer = reducer{
	name: "listing", version: 1, keepErrors: true,
	detect: func(lines []string) bool {
		n, long, tree, find := 0, 0, 0, 0
		for _, l := range lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			n++
			switch {
			case listLong.MatchString(l):
				long++
			case listTree.MatchString(l):
				tree++
			case listFind.MatchString(l) && strings.Contains(l, "/"):
				find++
			}
		}
		return n >= minReducerLines && (long*10 >= n*8 || tree*10 >= n*8 || find*10 >= n*9)
	},
	plan: func(lines []string) cutPlan {
		drop := make([]bool, len(lines))
		perDir := map[string]int{}
		var treePath []string
		for i, l := range lines {
			var dir string
			depth := 0
			switch {
			case listLong.MatchString(l):
				dir = "."
			case listTree.MatchString(l):
				m := listTree.FindStringSubmatch(l)
				depth = len([]rune(m[1]))/4 + 1
				name := strings.TrimPrefix(l, m[0])
				if depth > len(treePath) {
					treePath = append(treePath, make([]string, depth-len(treePath))...)
				}
				treePath = append(treePath[:depth-1], name)
				dir = strings.Join(treePath[:depth-1], "/")
				if listNoise.MatchString("/" + strings.Join(treePath[:depth-1], "/") + "/") {
					drop[i] = true
					continue
				}
			case listFind.MatchString(l) && strings.Contains(l, "/"):
				p := strings.TrimPrefix(l, "./")
				depth = strings.Count(p, "/") + 1
				dir = p[:max(strings.LastIndexByte(p, '/'), 0)]
				if listNoise.MatchString("/" + dir + "/") {
					drop[i] = true
					continue
				}
			default:
				continue
			}
			perDir[dir]++
			drop[i] = depth > listDepth || perDir[dir] > listPerDir
		}
		return planFromDrops(drop)
	},
}

// Build logs (docker build, npm, pip, Maven / Gradle compile): the last
// buildTail lines, step headers and every error stay.
const buildTail = 20

var (
	buildDocker = regexp.MustCompile(`^#\d+ `)
	buildStep   = regexp.MustCompile(`^#\d+ \[|^Step \d+/\d+ :`)
	buildNpm    = regexp.MustCompile(`^npm (warn|WARN|ERR!|notice|info|http)\b`)
	buildPip    = regexp.MustCompile(`^\s*(Collecting|Downloading|Installing collected|Requirement already satisfied|Using cached|Building wheel)\b`)
	buildMaven  = regexp.MustCompile(`^\[(INFO|WARNING|ERROR)\] `)
	buildDone   = regexp.MustCompile(`^(BUILD (SUCCESS|FAILURE|SUCCESSFUL|FAILED)|\[INFO\] BUILD (SUCCESS|FAILURE)|#\d+ (DONE|ERROR)|added \d+ packages|Successfully (installed|built))`)
)

var buildLogReducer = reducer{
	name: "build log", version: 1, keepErrors: true,
	detect: func(lines []string) bool {
		n := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				n++
			}
		}
		shaped := countMatches(lines, buildDocker, buildNpm, buildPip, buildMaven)
		return n >= 2*buildTail && shaped*10 >= n*5 && countMatches(lines, buildDone) > 0
	},
	plan: func(lines []string) cutPlan {
		drop := make([]bool, len(lines))
		for i, l := range lines {
			drop[i] = i < len(lines)-buildTail && !buildStep.MatchString(l) && strings.TrimSpace(l) != ""
		}
		return planFromDrops(drop)
	},
}

// Stack traces: runs of framework frames (the language runtime, test
// runners, dependencies) fold to a count; the first and last frame of each
// trace and every frame in user code stay.
var (
	frameJava   = regexp.MustCompile(`^\s+at [\w$./<>-]+\(.*\)$`)
	frameJS     = regexp.MustCompile(`^\s+at .*\(?\S+:\d+:\d+\)?$`)
	framePython = regexp.MustCompile(`^\s+File ".+", line \d+, in \S+`)
	frameGoFile = regexp.MustCompile(`^\t/\S+\.go:\d+( \+0x[0-9a-f]+)?$`)
	framework   = regexp.MustCompile(`(^\s+at (java|javax|jdk|sun|kotlin|scala|org\.junit|org\.springframework|org\.apache|org\.gradle|io\.netty|reactor)\.)|node_modules/|node:internal|site-packages/|/lib/python\d|/usr/local/go/src/|/go/pkg/mod/|/runtime/|/testing/`)
)

var stackTraceReducer = reducer{
	name: "stack trace", version: 1, keepErrors: true,
	detect: func(lines []string) bool {
		return countMatches(lines, frameJava, frameJS, framePython, frameGoFile) >= 5 && countMatches(lines, framework) >= 3
	},
	plan: func(lines []string) cutPlan {
		// frames[i] is the frame a line belongs to: -1 for none. Python
		// and Go frames are two lines (location and code, function and
		// file).
		frame := make([]int, len(lines))
		fw := map[int]bool{}
		next := 0
		for i := range lines {
			frame[i] = -1
		}
		for i := 0; i < len(lines); i++ {
			l := lines[i]
			switch {
			case frameJava.MatchString(l), frameJS.MatchString(l):
				frame[i] = next
				fw[next] = framework.MatchString(l)
				next++
			case framePython.MatchString(l):
				frame[i] = next
				fw[next] = framework.MatchString(l)
				if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ") && !framePython.MatchString(lines[i+1]) {
					i++
					frame[i] = next
				}
				next++
			case i+1 < len(lines) && frameGoFile.MatchString(lines[i+1]) && !strings.HasPrefix(l, "\t"):
				frame[i], frame[i+1] = next, next
				fw[next] = framework.MatchString(lines[i+1])
				i++
				next++
			}
		}
		// A trace is a run of frames with nothing else between them; its
		// first and last frames stay.
		drop := make([]bool, len(lines))
		notes := map[int]note{}
		for i := 0; i < len(lines); {
			if frame[i] < 0 {
				i++
				continue
			}
			j := i
			var frames []int
			for j < len(lines) && frame[j] >= 0 {
				if len(frames) == 0 || frames[len(frames)-1] != frame[j] {
					frames = append(frames, frame[j])
				}
				j++
			}
			if len(frames) > 2 {
				first, last := frames[0], frames[len(frames)-1]
				// Fold runs of two or more framework frames.
				k := i
				for k < j {
					f := frame[k]
					if f == first || f == last || !fw[f] {
						k++
						continue
					}
					start, n, count := k, 0, 0
					for k < j && frame[k] != last && fw[frame[k]] {
						if k == start || frame[k] != frame[k-1] {
							count++
						}
						k++
						n++
					}
					if count >= 2 {
						for x := start; x < start+n; x++ {
							drop[x] = true
						}
						notes[start] = note{n: n, text: fmt.Sprintf("[tyk-compressed: %d framework frames]", count)}
					}
				}
			}
			i = j
		}
		return cutPlan{drop: drop, notes: notes, failures: -1}
	},
}

// Repeated lines: a run of identical lines folds to the first and a count.
// It does not keep repeated error lines: the first stays and the note says
// how many followed, so the text survives without its copies.
const minRepeat = 3

var repeatedReducer = reducer{
	name: "repeated lines", version: 1, keepErrors: false,
	detect: func(lines []string) bool {
		run := 1
		for i := 1; i < len(lines); i++ {
			if lines[i] == lines[i-1] && strings.TrimSpace(lines[i]) != "" {
				run++
				if run >= minRepeat+1 {
					return true
				}
			} else {
				run = 1
			}
		}
		return false
	},
	plan: func(lines []string) cutPlan {
		drop := make([]bool, len(lines))
		notes := map[int]note{}
		for i := 0; i < len(lines); {
			j := i + 1
			for j < len(lines) && lines[j] == lines[i] {
				j++
			}
			if n := j - i; n >= minRepeat && strings.TrimSpace(lines[i]) != "" {
				for k := i + 1; k < j; k++ {
					drop[k] = true
				}
				notes[i+1] = note{n: n - 1, text: fmt.Sprintf("[tyk-compressed: previous line repeated %d more times]", n-1)}
			}
			i = j
		}
		return cutPlan{drop: drop, notes: notes, failures: -1}
	},
}
