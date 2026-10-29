package census

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Unmatched — категорія вибірок профілю, чий стек не збігся з жодним місцем.
const Unmatched = "unmatched"

var (
	// ErrAllocProfile означає, що текст не є профілем `go tool pprof -raw`
	// зі стовпцями alloc_objects і alloc_space.
	ErrAllocProfile = errors.New("немає alloc_objects або alloc_space")

	// ErrProfileLocation означає, що вибірка посилається на id локації,
	// якого в профілі немає.
	ErrProfileLocation = errors.New("немає локації профілю")
)

// Frame — один кадр стека алокації. У вибірці кадри йдуть від листа до викликача.
type Frame struct {
	File string
	Line int
}

// AllocSample — одна вибірка профілю: alloc_objects, alloc_space і стек.
type AllocSample struct {
	Objects int64
	Bytes   int64
	Frames  []Frame
}

// Weight — частка однієї категорії: кількість місць і віднесені алокації.
type Weight struct {
	Category string
	Sites    int
	Objects  int64
	Bytes    int64
}

// pprof -raw: «id: addr M=n func file:line:col s=start», далі рядки інлайну без адреси.
var (
	sampleLineRE = regexp.MustCompile(`^\s*(\d+(?:\s+\d+)*)\s*:\s*(.*)$`)
	// Повне ім'я функції pprof дописує в дужках одразу після s=рядок.
	// Узагальнений тип тримає власні дужки, тож група йде до кінця рядка.
	locHeadRE = regexp.MustCompile(`^\s*(\d+):\s+0x[0-9a-fA-F]+\s+M=\d+\s+(.+)\s+(\S+):(\d+):\d+(?:\s+s=\d+)?(?:\(.*\))?\s*$`)
	locContRE = regexp.MustCompile(`^\s+(\S.*?)\s+(\S+):(\d+):\d+(?:\s+s=\d+)?(?:\(.*\))?\s*$`)
)

// ParseAllocProfile читає текст `go tool pprof -raw` і повертає вибірки
// alloc_objects і alloc_space. Стек кожної вибірки йде від листа до кореня,
// разом із кадрами інлайну. Вибірка з нулями в обох стовпцях пропускається.
// Успіх без жодної вибірки дає порожній зріз, не nil.
func ParseAllocProfile(r io.Reader) ([]AllocSample, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)

	const (
		secNone = iota
		secSamples
		secLocs
	)
	section := secNone
	objectsCol, bytesCol := -1, -1
	var pending []rawSample
	locs := map[int][]Frame{}
	current := 0

	for sc.Scan() {
		line := sc.Text()
		switch strings.TrimSpace(line) {
		case "Samples:":
			section = secSamples
			continue
		case "Locations":
			section = secLocs
			continue
		case "Mappings":
			section = secNone
			continue
		}
		var err error
		switch section {
		case secSamples:
			objectsCol, bytesCol, pending, err = takeSample(line, objectsCol, bytesCol, pending)
		case secLocs:
			current, err = takeLocation(line, locs, current)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("читання профілю: %w", err)
	}
	if objectsCol < 0 || bytesCol < 0 {
		return nil, ErrAllocProfile
	}
	return samplesFrom(pending, locs)
}

type rawSample struct {
	objects int64
	bytes   int64
	ids     []int
}

// takeSample читає заголовок стовпців або один рядок вибірки.
func takeSample(line string, objectsCol, bytesCol int, pending []rawSample) (int, int, []rawSample, error) {
	if objectsCol < 0 {
		objectsCol, bytesCol, ok := sampleColumns(line)
		if !ok {
			return -1, -1, pending, nil
		}
		if objectsCol < 0 || bytesCol < 0 {
			return -1, -1, pending, ErrAllocProfile
		}
		return objectsCol, bytesCol, pending, nil
	}
	sample, ok, err := parseRawSample(line, objectsCol, bytesCol)
	if err != nil || !ok {
		return objectsCol, bytesCol, pending, err
	}
	return objectsCol, bytesCol, append(pending, sample), nil
}

func sampleColumns(line string) (objectsCol, bytesCol int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 || !strings.Contains(fields[0], "/") {
		return -1, -1, false
	}
	objectsCol, bytesCol = -1, -1
	for i, field := range fields {
		name, _, _ := strings.Cut(field, "/")
		name, _, _ = strings.Cut(name, "[")
		switch name {
		case "alloc_objects":
			objectsCol = i
		case "alloc_space":
			bytesCol = i
		}
	}
	return objectsCol, bytesCol, true
}

func parseRawSample(line string, objectsCol, bytesCol int) (rawSample, bool, error) {
	m := sampleLineRE.FindStringSubmatch(line)
	if m == nil {
		return rawSample{}, false, nil
	}
	values := strings.Fields(m[1])
	if len(values) <= max(objectsCol, bytesCol) {
		return rawSample{}, false, fmt.Errorf("стовпці вибірки %q: %w", strings.TrimSpace(line), ErrAllocProfile)
	}
	objects, err := strconv.ParseInt(values[objectsCol], 10, 64)
	if err != nil {
		return rawSample{}, false, fmt.Errorf("alloc_objects %q: %w", values[objectsCol], ErrAllocProfile)
	}
	space, err := strconv.ParseInt(values[bytesCol], 10, 64)
	if err != nil {
		return rawSample{}, false, fmt.Errorf("alloc_space %q: %w", values[bytesCol], ErrAllocProfile)
	}
	var ids []int
	for field := range strings.FieldsSeq(m[2]) {
		id, err := strconv.Atoi(field)
		if err != nil {
			return rawSample{}, false, fmt.Errorf("локація %q: %w", field, ErrProfileLocation)
		}
		ids = append(ids, id)
	}
	return rawSample{objects: objects, bytes: space, ids: ids}, true, nil
}

func takeLocation(line string, locs map[int][]Frame, current int) (int, error) {
	if m := locHeadRE.FindStringSubmatch(line); m != nil {
		id, err := strconv.Atoi(m[1])
		if err != nil {
			return current, fmt.Errorf("локація %q: %w", m[1], ErrProfileLocation)
		}
		lineNo, err := strconv.Atoi(m[4])
		if err != nil {
			return current, fmt.Errorf("рядок %q: %w", m[4], ErrAllocProfile)
		}
		locs[id] = []Frame{{File: m[3], Line: lineNo}}
		return id, nil
	}
	if current == 0 {
		return current, nil
	}
	m := locContRE.FindStringSubmatch(line)
	if m == nil {
		return current, nil
	}
	lineNo, err := strconv.Atoi(m[3])
	if err != nil {
		return current, fmt.Errorf("рядок %q: %w", m[3], ErrAllocProfile)
	}
	locs[current] = append(locs[current], Frame{File: m[2], Line: lineNo})
	return current, nil
}

func samplesFrom(pending []rawSample, locs map[int][]Frame) ([]AllocSample, error) {
	out := []AllocSample{}
	for _, sample := range pending {
		var frames []Frame
		for _, id := range sample.ids {
			got, ok := locs[id]
			if !ok {
				return nil, fmt.Errorf("локація %d: %w", id, ErrProfileLocation)
			}
			frames = append(frames, got...)
		}
		if sample.objects == 0 && sample.bytes == 0 {
			continue
		}
		out = append(out, AllocSample{Objects: sample.objects, Bytes: sample.bytes, Frames: frames})
	}
	return out, nil
}

// Weigh відносить кожну вибірку до першого кадра від листа до кореня, чий
// файл і рядок збігаються з місцем census. Кілька місць на одному рядку
// ділять об'єкти і байти порівну; остача дістається місцям, що стоять раніше
// в sites. Вибірка без збігу має категорію Unmatched. Порожній вхід дає
// порожній зріз, не nil. Порядок рядків — каскад v0, далі інші категорії
// за назвою, Unmatched наприкінці.
func Weigh(sites []Site, samples []AllocSample) []Weight {
	type placed struct {
		file string
		line int
		ok   bool
	}
	where := make([]placed, len(sites))
	type lineKey struct {
		base string
		line int
	}
	index := map[lineKey][]int{}
	for i, site := range sites {
		file, line, ok := splitPos(site.Pos)
		where[i] = placed{file: file, line: line, ok: ok}
		if !ok {
			continue
		}
		key := lineKey{base: filepath.Base(file), line: line}
		index[key] = append(index[key], i)
	}

	objects := make([]int64, len(sites))
	space := make([]int64, len(sites))
	var unmatchedObjects, unmatchedBytes int64
	for _, sample := range samples {
		if sample.Objects == 0 && sample.Bytes == 0 {
			continue
		}
		var hits []int
		for _, frame := range sample.Frames {
			cands := index[lineKey{base: filepath.Base(frame.File), line: frame.Line}]
			for _, i := range cands {
				if where[i].ok && where[i].line == frame.Line && sameFile(where[i].file, frame.File) {
					hits = append(hits, i)
				}
			}
			if len(hits) > 0 {
				break
			}
		}
		if len(hits) == 0 {
			unmatchedObjects += sample.Objects
			unmatchedBytes += sample.Bytes
			continue
		}
		share(objects, hits, sample.Objects)
		share(space, hits, sample.Bytes)
	}

	type acc struct {
		sites   int
		objects int64
		bytes   int64
	}
	by := map[string]acc{}
	for i, site := range sites {
		got := by[site.Category]
		got.sites++
		got.objects += objects[i]
		got.bytes += space[i]
		by[site.Category] = got
	}
	out := []Weight{}
	for cat, got := range by {
		out = append(out, Weight{
			Category: cat,
			Sites:    got.sites,
			Objects:  got.objects,
			Bytes:    got.bytes,
		})
	}
	if unmatchedObjects != 0 || unmatchedBytes != 0 {
		out = append(out, Weight{Category: Unmatched, Objects: unmatchedObjects, Bytes: unmatchedBytes})
	}
	rank := map[string]int{
		"stack": 0, "static": 1, "rc-local": 2, "rc-shared": 3,
		"cyclic": 4, "pinned": 5, "implicit": 6,
	}
	slices.SortFunc(out, func(a, b Weight) int {
		if (a.Category == Unmatched) != (b.Category == Unmatched) {
			if a.Category == Unmatched {
				return 1
			}
			return -1
		}
		ar, aok := rank[a.Category]
		br, bok := rank[b.Category]
		if aok != bok {
			if aok {
				return -1
			}
			return 1
		}
		if aok && ar != br {
			return ar - br
		}
		return strings.Compare(a.Category, b.Category)
	})
	return out
}

// share ділить total між hits. Остача дістається ранішим індексам.
func share(dst []int64, hits []int, total int64) {
	n := int64(len(hits))
	base := total / n
	rem := total % n
	for i, hit := range hits {
		part := base
		if int64(i) < rem {
			part++
		}
		dst[hit] += part
	}
}

// FormatWeights друкує таблицю часток. Відсотки місць рахуються від усіх місць,
// відсотки об'єктів і байтів — лише від вибірок, віднесених до місць census.
// Unmatched у таблицю не входить: якщо такі вибірки є, під таблицею окремий рядок.
func FormatWeights(weights []Weight) string {
	var sites int
	var attrObjects, attrBytes int64
	var unmatched Weight
	var hasUnmatched bool
	for _, weight := range weights {
		if weight.Category == Unmatched {
			unmatched = weight
			hasUnmatched = true
			continue
		}
		sites += weight.Sites
		attrObjects += weight.Objects
		attrBytes += weight.Bytes
	}
	percent := func(part, total int64) string {
		if total == 0 {
			return "0.0"
		}
		return strconv.FormatFloat(float64(part)*100/float64(total), 'f', 1, 64)
	}
	var b strings.Builder
	b.WriteString("| Категорія | Місця | % місць | Об'єкти | % об'єктів | Байти | % байтів |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, weight := range weights {
		if weight.Category == Unmatched {
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %d | %s | %d | %s |\n",
			weight.Category,
			weight.Sites, percent(int64(weight.Sites), int64(sites)),
			weight.Objects, percent(weight.Objects, attrObjects),
			weight.Bytes, percent(weight.Bytes, attrBytes),
		)
	}
	if hasUnmatched && (unmatched.Objects != 0 || unmatched.Bytes != 0) {
		totalObjects := attrObjects + unmatched.Objects
		totalBytes := attrBytes + unmatched.Bytes
		fmt.Fprintf(&b, "\nПоза census: об'єктів %d (%s%% профілю), байтів %d (%s%% профілю).\n",
			unmatched.Objects, percent(unmatched.Objects, totalObjects),
			unmatched.Bytes, percent(unmatched.Bytes, totalBytes),
		)
	}
	return b.String()
}
