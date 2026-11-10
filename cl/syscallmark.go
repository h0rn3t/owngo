/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cl

import (
	"golang.org/x/tools/go/ssa"
)

// Маркери блокувальних викликів C під owngomn (P-NEW-09.8): виклик стоїть між
// EnterSyscall і ExitSyscall, щоб sysmon міг забрати P у M, що застрягла в
// C, а вхід //export — між CallbackEnter і CallbackExit, щоб G, яка
// блокується в callback, повернулася в C на тому самому потоці. Рантайм
// блокується сам через psync.BlockEnter, тож його пакети не маркуються.

// nonBlockingCFuncs — функції C, які не блокуються: маркер лише коштував би
// часу.
var nonBlockingCFuncs = map[string]bool{
	"memcpy": true, "memmove": true, "memset": true, "memcmp": true,
	"strlen": true, "strcmp": true, "strncmp": true,
	"malloc": true, "calloc": true, "realloc": true, "free": true,
	"cliteErrno": true, "__errno_location": true, "__error": true,
	"abort": true, "exit": true, "_exit": true,
}

// marksBlockingCall — чи маркувати виклик функції C cName з поточної функції.
func (p *context) marksBlockingCall(cName string) bool {
	return p.prog.BlockingCallMarkersEnabled() && !nonBlockingCFuncs[cName] && p.marksPackage(p.goFn)
}

func (p *context) marksPackage(fn *ssa.Function) bool {
	path := safepointPackagePath(fn)
	return path != "" && !excludeSafepointPackage(path)
}

func (p *context) prepareCallbackEntry(f *ssa.Function) {
	p.callbackEntry = false
	if !p.prog.BlockingCallMarkersEnabled() || f == nil || f.Pkg == nil || !p.marksPackage(f) {
		return
	}
	_, p.callbackEntry = p.pkg.ExportFuncs()[funcName(f.Pkg.Pkg, f, false)]
}
