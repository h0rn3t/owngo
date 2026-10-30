package build

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xgo-dev/llgo/internal/crosscompile"
)

func TestCommandEnvLookupUsesLastValue(t *testing.T) {
	environ := []string{"CC=first", "malformed", "CXX=clang++", "CC=last"}
	commands := commandEnv{environ: environ}
	if got := commands.lookup("CC"); got != "last" {
		t.Fatalf("lookup(CC) = %q, want last", got)
	}
	if got := commands.lookup("CXX"); got != "clang++" {
		t.Fatalf("lookup(CXX) = %q, want clang++", got)
	}
	if got := commands.lookup("MISSING"); got != "" {
		t.Fatalf("lookup(MISSING) = %q", got)
	}
}

func TestNativeToolchainCommands(t *testing.T) {
	if os.Getenv("LLGO_NATIVE_COMMAND_HELPER") == "1" {
		separator := slices.Index(os.Args, "--")
		if separator < 0 {
			os.Exit(2)
		}
		_, _ = fmt.Fprint(os.Stdout, strings.Join(os.Args[separator+1:], " "))
		os.Exit(0)
	}

	helper := func(kind string) []string {
		return []string{os.Args[0], "-test.run=^TestNativeToolchainCommands$", "--", kind}
	}
	cc := helper("cc")
	cxx := helper("cxx")
	ctx := &context{
		buildConf: &Config{},
		crossCompile: crosscompile.Export{
			CC:      cc[0],
			CCArgs:  cc[1:],
			CXX:     cxx[0],
			CXXArgs: cxx[1:],
		},
		commands: commandEnv{
			dir:     t.TempDir(),
			environ: append(os.Environ(), "LLGO_NATIVE_COMMAND_HELPER=1"),
		},
	}
	run := func(name, want string, command func(*bytes.Buffer) error) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := command(&output); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != want {
				t.Fatalf("command arguments = %q, want %q", got, want)
			}
		})
	}
	run("C source", "cc input", func(output *bytes.Buffer) error {
		cmd := ctx.compilerForSource("source.c")
		cmd.Stdout, cmd.Stderr = output, output
		return cmd.Compile("input")
	})
	run("C++ source", "cxx input", func(output *bytes.Buffer) error {
		cmd := ctx.compilerForSource("source.cpp")
		cmd.Stdout, cmd.Stderr = output, output
		return cmd.Compile("input")
	})
	run("default linker", "cxx input", func(output *bytes.Buffer) error {
		cmd := ctx.linker()
		cmd.Stdout, cmd.Stderr = output, output
		return cmd.Link("input")
	})

	extld := helper("extld")
	ctx.crossCompile.Linker = extld[0]
	ctx.crossCompile.LinkerArgs = extld[1:]
	run("explicit external linker", "extld input", func(output *bytes.Buffer) error {
		cmd := ctx.linker()
		cmd.Stdout, cmd.Stderr = output, output
		return cmd.Link("input")
	})
}

func TestIRCompilerUsesMatchingLLVMForEmscriptenMemory64(t *testing.T) {
	ctx := &context{crossCompile: crosscompile.Export{
		CC:      "emcc",
		CCArgs:  []string{"--emcc-prefix"},
		CCFLAGS: []string{"-target", "wasm64-unknown-emscripten"},
	}}
	config := ctx.irClangConfig()
	if config.CC != "emcc" || !reflect.DeepEqual(config.CCArgs, []string{"--emcc-prefix"}) {
		t.Fatalf("default IR compiler = %q %q, want emcc command", config.CC, config.CCArgs)
	}

	ctx.crossCompile.WasmProfile = crosscompile.WasmProfileJ64
	ctx.crossCompile.WasmProvider = crosscompile.WasmProviderEmscripten
	config = ctx.irClangConfig()
	if config.CC != "clang" || len(config.CCArgs) != 0 {
		t.Fatalf("Memory64 IR compiler = %q %q, want matching LLVM clang", config.CC, config.CCArgs)
	}
	wantFlags := []string{
		"-target", "wasm64-unknown-emscripten",
		"-mllvm", "-combiner-global-alias-analysis=false",
		"-mllvm", "-enable-emscripten-sjlj",
		"-mllvm", "-disable-lsr",
	}
	if !reflect.DeepEqual(config.CCFLAGS, wantFlags) {
		t.Fatalf("Memory64 IR compiler flags = %q", config.CCFLAGS)
	}
}
