// mandelbrot за зразком програми Rust #4 з Benchmarks Game (Matt Watson,
// TeXitoi, Volodymyr M. Lisivka, Michael Cicotti, Ryohei Machida), Revised
// BSD, див. LICENSE:
// https://benchmarksgame-team.pages.debian.net/benchmarksgame/program/mandelbrot-rust-4.html
//
// Rust #4 рахує вісім пікселів разом у F64x8 і покладається на
// автовекторизацію LLVM. Тут той самий вектор — [8]float64 з циклами по
// восьми елементах. Робота поділена так само: рядки бере пул робітників,
// як par_chunks_mut у rayon.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

// mand8 повертає байт рядка PBM для восьми пікселів з дійсними частинами
// cr і уявною ci: біт k — піксель cr[k], 1 — точка в множині. Якщо
// попередній байт рядка нульовий, вихід раніший, коли розбіглися всі вісім.
//
// Мова дозволяє зливати множення з додаванням навіть через присвоєння, а
// Rust не зливає. Явні float64(…) округлюють добутки, як у Rust, інакше
// вивід розійшовся б з очікуваним.
func mand8(cr [8]float64, ci float64, lastChar byte) byte {
	var zi, tr, ti [8]float64
	zr := cr
	for k := range 8 {
		zi[k] = ci
		tr[k] = float64(zr[k] * zr[k])
		ti[k] = float64(ci * ci)
	}
	// 12 кроків по 4 ітерації з перевіркою після кожного і одна остання.
	for i := 1; i <= 12*4+1; i++ {
		for k := range 8 {
			zi[k] = float64((zr[k]+zr[k])*zi[k]) + ci
			zr[k] = tr[k] - ti[k] + cr[k]
			tr[k] = float64(zr[k] * zr[k])
			ti[k] = float64(zi[k] * zi[k])
		}
		if i%4 != 0 || lastChar != 0 {
			continue
		}
		diverged := true
		for k := range 8 {
			// Не !(<= 4): NaN не вважається розбіжним, як у Rust.
			diverged = diverged && tr[k]+ti[k] > 4
		}
		if diverged {
			return 0
		}
	}
	var bits byte
	for k := range 8 {
		if tr[k]+ti[k] <= 4 {
			bits |= 1 << k
		}
	}
	return bits
}

// run повертає зображення size×size у форматі PBM P4; size округлюється
// вниз до кратного 8.
func run(size int) []byte {
	size = size / 8 * 8
	width := size / 8
	inv := 2 / float64(size)

	// Пікселі в байті йдуть від старшого біта, тож елемент 0 — найправіший.
	xloc := make([][8]float64, width)
	for i := range xloc {
		for k := range 8 {
			xloc[i][k] = float64(float64(i*8+7-k)*inv) - 1.5
		}
	}

	header := fmt.Sprintf("P4\n%d %d\n", size, size)
	out := make([]byte, len(header)+size*width)
	rows := out[copy(out, header):]

	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), size) {
		wg.Go(func() {
			for {
				y := int(next.Add(1) - 1)
				if y >= size {
					break
				}
				ci := float64(float64(y)*inv) - 1
				row := rows[y*width : (y+1)*width]
				var lastChar byte
				for x := range row {
					lastChar = mand8(xloc[x], ci, lastChar)
					row[x] = lastChar
				}
			}
		})
	}
	wg.Wait()
	return out
}

func main() {
	size := 200
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil && v >= 0 {
			size = v
		}
	}
	if _, err := os.Stdout.Write(run(size)); err != nil {
		os.Exit(1)
	}
}
