//go:build owngo && !owngoasan

package main

import (
	"sync"
	"unsafe"
)

const (
	blockSize = 48
	blocks    = 20000 // кілька десятків сторінок одного класу
)

// checkAllocator перевіряє три речі алокатора P8.3: адреси звільненого
// великого блока використовуються повторно; блоки, звільнені чужим потоком,
// повертаються живому власнику; сторінки потоку, що завершився, стають
// придатними для нових блоків. Пошкоджений блок — два виділення однієї адреси.
func checkAllocator() {
	large := allocU(4 << 20)
	freeRoot(large)
	if again := allocU(4 << 20); again != large {
		println("large reuse FAIL:", again, "want", large)
		failed = true
	} else {
		println("large reuse ok")
		freeRoot(again)
	}

	// Власник живий: main звільняє його блоки, власник виділяє знову.
	ptrs := make(chan []unsafe.Pointer)
	freed := make(chan struct{})
	result := make(chan bool)
	go func() {
		ptrs <- allocTagged()
		<-freed
		again := allocTagged()
		ok := tagsIntact(again)
		freeAll(again)
		result <- ok
	}()
	freeAll(<-ptrs)
	close(freed)
	report("remote free", <-result)

	// Власник завершився: його сторінки забирає потік, що звільняє.
	var wg sync.WaitGroup
	var orphan []unsafe.Pointer
	wg.Go(func() { orphan = allocTagged() })
	wg.Wait()
	freeAll(orphan)
	again := allocTagged()
	report("orphan pages", tagsIntact(again))
	freeAll(again)
}

// allocTagged виділяє blocks блоків і пише в кожен його номер.
func allocTagged() []unsafe.Pointer {
	ptrs := make([]unsafe.Pointer, blocks)
	for i := range ptrs {
		ptrs[i] = allocU(blockSize)
		*(*int)(ptrs[i]) = i
	}
	return ptrs
}

// tagsIntact перевіряє номери й базу кожного блока allocTagged.
func tagsIntact(ptrs []unsafe.Pointer) bool {
	for i, p := range ptrs {
		if *(*int)(p) != i || base(unsafe.Add(p, blockSize-1)) != p {
			return false
		}
	}
	return true
}

func freeAll(ptrs []unsafe.Pointer) {
	for _, p := range ptrs {
		freeRoot(p)
	}
}

func report(what string, ok bool) {
	if !ok {
		println(what, "FAIL")
		failed = true
		return
	}
	println(what, "ok")
}
