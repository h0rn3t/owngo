#!/usr/bin/env python3
"""Єдиний набір заміру треку швидкодії (S1.1).

Збирає програму корпусу в кожному варіанті — go, llgo без тега, llgo -tags
owngo з типовим -Os і з -O2 -lto full, Rust, C — і запускає кожен варіант RUNS
разів, чергуючи варіанти в кожному колі. Вивід кожного прогону звіряється з
testdata/expected-N.* Go-програми таблиці. Таблиці Markdown — у stdout, JSON з
усіма прогонами й оточенням — у файл --json.

binary-trees має дві таблиці: звичайні алокації (Go #2, C на malloc) і арени
(binarytreesarena, Rust #5, C на пулах APR). Висновок між таблицями не робиться. Для Go #2 під
owngo окремо міряється CPU-час на 18: на всіх ядрах і під taskset -c 0 (S1).

Лише Linux: ru_maxrss читається як КБ. Потрібні Go, LLVM 23 першим у PATH
(clang збирає rusage.c і програми C), cargo, taskset, libomp-23-dev (чи LIBOMP_ROOT),
libapr1-dev.

k-nucleotide, regex-redux і reverse-complement (корпус етапу C, P-NEW-02.2)
читають FASTA зі stdin, як на сторінках Benchmarks Game: вхід — вивід
owngo/bench/fasta N, який скрипт генерує один раз; аргумент програм — 0.
regex-redux C потребує заголовків PCRE2: libpcre2-dev чи PCRE2_ROOT (каталог
usr розпакованого libpcre2-dev).

З --profiles Go-варіанти — профілі збірки owngo (P-NEW-01): -Os, -O2, -O3,
-O2 -lto thin, -O2 -lto full. Для кожного Go-варіанта записуються розмір
бінарника і час холодної збірки: кожен варіант збирається з власним
XDG_CACHE_HOME (кеш llgo і go), який після збірки видаляється.

Використання: owngo/bench/compare.py PROGRAM [N] [RUNS] [--profiles] [--json FILE]
"""

import argparse
import hashlib
import json
import os
import platform
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BENCH = ROOT / "owngo" / "bench"
X86 = platform.machine() == "x86_64"

# Таблиці програми: назва, Go-пакет у owngo/bench, крейт Rust (чи None),
# підпис Rust і програма C (чи None): каталог у owngo/bench, підпис, прапорці
# clang понад C_FLAGS, аргументи перед N. Підписи Go-варіантів однакові для
# всіх програм.
PROGRAMS = {
    "binarytrees": {
        "n": 21,
        "tables": [
            ("regular", "binarytrees", None, None,
             ("binarytreesc", "C #5, malloc + pthreads", ["-pthread"], [])),
            ("arena", "binarytreesarena", "binarytreesrust", "Rust #5, bumpalo + rayon",
             ("binarytreescpool", "C #2, APR pools + OpenMP", ["OPENMP", "APR"], [])),
        ],
        # CPU-час Go #2 під owngo на 18 (S1): таблиця, варіант, N.
        "cpu_probe": ("regular", "owngo", 18),
    },
    "fannkuchredux": {
        "n": 12,
        # C #6 типово бере 4 потоки — усі ядра машини Benchmarks Game.
        "tables": [("main", "fannkuchredux", "fannkuchreduxrust", "Rust #6, rayon",
                    ("fannkuchreduxc", "C #6, pthreads", ["-pthread"],
                     ["-t", str(os.cpu_count())]))],
    },
    # Корпус етапу C (P-NEW-02.2): вхід — fasta N через stdin.
    "knucleotide": {
        "n": 25000000,
        "stdin": True,
        "tables": [("main", "knucleotide", "knucleotiderust",
                    "Rust #7, hashbrown + tokio-threadpool",
                    ("knucleotidec", "C #1, khash + OpenMP",
                     ["OPENMP", "-Iowngo/bench/knucleotidec/Include"], []))],
    },
    "regexredux": {
        "n": 5000000,
        "stdin": True,
        "tables": [("main", "regexredux", "regexreduxrust", "Rust #7, PCRE2 JIT + rayon",
                    ("regexreduxc", "C #5, PCRE2 JIT + OpenMP", ["OPENMP", "PCRE2"], []))],
    },
    "revcomp": {
        "n": 100000001,
        "stdin": True,
        "tables": [("main", "revcomp", "revcomprust", "Rust #1, rayon",
                    ("revcompc", "C #7, pthreads", ["-pthread"], []))],
    },
    "mandelbrot": {
        "n": 16000,
        # C #6 оголошує mand8 як C99 inline без зовнішнього визначення: gcc
        # вбудовує кожен виклик, clang — ні, тож -fgnu89-inline дає визначення.
        "tables": [("main", "mandelbrot", "mandelbrotrust", "Rust #4, rayon",
                    ("mandelbrotc", "C #6, OpenMP",
                     ["OPENMP", "-mno-fma", "-fno-finite-math-only", "-fgnu89-inline"], []))],
    },
}

TABLE_TITLES = {
    "regular": "звичайні алокації",
    "arena": "арени",
}

# Варіанти Go-програми: ключ, підпис, аргументи llgo build (None — go build).
GO_VARIANTS = [
    ("go", "go, GOAMD64=v2" if X86 else "go", None),
    ("llgo", "llgo", []),
    ("owngo", "llgo -tags owngo (-Os)", ["-tags", "owngo"]),
    # Профіль треку (P-NEW-01).
    ("owngo-thin", "llgo -tags owngo -O2 -lto thin", ["-tags", "owngo", "-O2", "-lto", "thin"]),
    ("owngo-lto", "llgo -tags owngo -O2 -lto full", ["-tags", "owngo", "-O2", "-lto", "full"]),
]

# Профілі збірки owngo (P-NEW-01). Ключ owngo — типовий -Os, як у GO_VARIANTS:
# на нього посилається cpu_probe.
PROFILE_VARIANTS = [
    ("owngo", "llgo -tags owngo (-Os)", ["-tags", "owngo"]),
    ("owngo-O2", "llgo -tags owngo -O2", ["-tags", "owngo", "-O2"]),
    ("owngo-O3", "llgo -tags owngo -O3", ["-tags", "owngo", "-O3"]),
    ("owngo-thin", "llgo -tags owngo -O2 -lto thin", ["-tags", "owngo", "-O2", "-lto", "thin"]),
    ("owngo-lto", "llgo -tags owngo -O2 -lto full", ["-tags", "owngo", "-O2", "-lto", "full"]),
]

# Прапори сторінок Benchmarks Game: Go — GOAMD64=v2, Rust — release з
# codegen-units=1 (Cargo.toml) і target-cpu=ivybridge.
GO_ENV = {"GOAMD64": "v2"} if X86 else {}
RUST_ENV = {"RUSTFLAGS": "-C target-cpu=ivybridge"} if X86 else {}
# C — прапорці gcc сторінок, але збирає clang з LLVM 23. OpenMP — libomp
# (libomp-23-dev): з -fopenmp=libgomp clang ігнорує прагми і програма йде в
# один потік. LIBOMP_ROOT — каталог llvm-23 з розпакованого libomp-23-dev, коли
# його не встановлено в систему.
C_FLAGS = ["-O3", "-fomit-frame-pointer"] + (["-march=ivybridge"] if X86 else [])


def openmp_flags():
    root = os.environ.get("LIBOMP_ROOT")
    if not root:
        return ["-fopenmp"]
    return ["-fopenmp", "-idirafter", f"{root}/lib/clang/23/include",
            f"-L{root}/lib", f"-Wl,-rpath,{root}/lib"]


def pcre2_flags():
    root = os.environ.get("PCRE2_ROOT")
    if not root:
        return output(["pkg-config", "--cflags", "--libs", "libpcre2-8"]).split()
    return [f"-I{root}/include", f"-L{root}/lib/{platform.machine()}-linux-gnu", "-lpcre2-8"]


C_EXPANSIONS = {
    "OPENMP": openmp_flags,
    "PCRE2": pcre2_flags,
    "APR": lambda: output(["pkg-config", "--cflags", "--libs", "apr-1"]).split(),
}

# Прапори ISA процесора, що впливають на codegen варіантів.
ISA_FLAGS = ["sse4_2", "popcnt", "avx", "avx2", "fma", "bmi2", "avx512f"]


def run(cmd, env=None, cwd=ROOT):
    subprocess.run(cmd, cwd=cwd, env={**os.environ, **(env or {})}, check=True)


def output(cmd):
    try:
        return subprocess.run(cmd, capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return None


def first_line(cmd):
    out = output(cmd)
    return out.splitlines()[0] if out else None


def environment():
    cpu_model, flags = None, []
    for line in Path("/proc/cpuinfo").read_text().splitlines():
        key, _, value = line.partition(":")
        if key.strip() == "model name" and cpu_model is None:
            cpu_model = value.strip()
        if key.strip() == "flags" and not flags:
            flags = value.split()
    sha = output(["git", "-C", str(ROOT), "rev-parse", "HEAD"])
    dirty = bool(output(["git", "-C", str(ROOT), "status", "--porcelain", "--untracked-files=no"]))
    return {
        "sha": sha,
        "dirty": dirty,
        "go": output(["go", "version"]),
        "llvm": output(["llvm-config", "--version"]),
        "clang": first_line(["clang", "--version"]),
        "rustc": output(["rustc", "--version"]),
        "cpu": cpu_model,
        "isa": [f for f in ISA_FLAGS if f in flags],
        "cores": os.cpu_count(),
        "ulimit": output(["sh", "-c", "ulimit -a"]),
        "go_env": GO_ENV,
        "rust_env": RUST_ENV,
        "c_flags": C_FLAGS,
        "libomp_root": os.environ.get("LIBOMP_ROOT"),
        "pcre2_root": os.environ.get("PCRE2_ROOT"),
    }


def expected_check(pkg, n):
    """Повертає функцію, що звіряє файл виводу з очікуваним, або None."""
    testdata = BENCH / pkg / "testdata"
    digest = testdata / f"expected-{n}.sha256"
    if digest.exists():
        want = digest.read_text().strip()
        return lambda path: hashlib.sha256(path.read_bytes()).hexdigest() == want
    for path in sorted(testdata.glob(f"expected-{n}.*")):
        want = path.read_bytes()
        return lambda out, want=want: out.read_bytes() == want
    return None


def build(spec, work, go_variants):
    """Збирає всі варіанти всіх таблиць; повертає таблиці з шляхами бінарників."""
    run(["clang", "-O2", "-o", str(work / "rusage"), str(BENCH / "rusage.c")])
    llgo = work / "llgo"
    run(["go", "build", "-o", str(llgo), "./cmd/llgo"])
    tables = []
    for name, pkg, crate, rust_label, c in spec["tables"]:
        variants = []
        for key, label, flags in go_variants:
            exe = work / f"{pkg}-{key}"
            cache = work / f"cache-{pkg}-{key}"
            start = time.monotonic()
            if flags is None:
                run(["go", "build", "-o", str(exe), f"./owngo/bench/{pkg}"],
                    env={**GO_ENV, "XDG_CACHE_HOME": str(cache)})
            else:
                run([str(llgo), "build", *flags, "-o", str(exe), f"./owngo/bench/{pkg}"],
                    env={"LLGO_ROOT": str(ROOT), "XDG_CACHE_HOME": str(cache)})
            build_s = round(time.monotonic() - start, 1)
            shutil.rmtree(cache, ignore_errors=True)
            variants.append({"key": key, "label": f"{pkg}, {label}", "exe": exe,
                             "build_s": build_s, "size_kb": exe.stat().st_size // 1024})
        if crate:
            target = work / "cargo"
            run(["cargo", "build", "--quiet", "--release", "--locked",
                 "--manifest-path", str(BENCH / crate / "Cargo.toml"),
                 "--target-dir", str(target)], env=RUST_ENV)
            exe = work / crate
            shutil.copy(target / "release" / crate.removesuffix("rust"), exe)
            variants.append({"key": "rust", "label": rust_label, "exe": exe})
        if c:
            cdir, c_label, extra, args = c
            exe = work / cdir
            flags = [f for e in extra for f in (C_EXPANSIONS[e]() if e in C_EXPANSIONS else [e])]
            run(["clang", *C_FLAGS, str(BENCH / cdir / "main.c"), "-o", str(exe), *flags])
            variants.append({"key": "c", "label": c_label, "exe": exe, "args": args,
                             "cflags": C_FLAGS + flags})
        tables.append({"name": name, "pkg": pkg, "variants": variants})
    return tables


def fasta_input(work, n):
    """Генерує вхід програм зі stdin: вивід owngo/bench/fasta N."""
    exe = work / "fasta"
    run(["go", "build", "-o", str(exe), "./owngo/bench/fasta"])
    path = work / f"fasta-{n}.txt"
    with path.open("wb") as f:
        subprocess.run([str(exe), str(n)], stdout=f, check=True)
    return path


def measure(rusage, cmd, out, stdin=None):
    """Запускає cmd з stdout у файл out (і stdin з файлу stdin) через rusage.c;
    повертає час, CPU і пікову RSS."""
    with open(stdin or os.devnull, "rb") as f:
        line = subprocess.run([str(rusage), str(out), *cmd], stdin=f, capture_output=True,
                              text=True, check=True).stdout
    wall, user, sys_, rss_kb, code = line.split()
    return {
        "wall": float(wall),
        "user": float(user),
        "sys": float(sys_),
        "rss_mb": round(int(rss_kb) / 1024, 1),
        "exit": int(code),
    }


def summarize(runs):
    walls = [r["wall"] for r in runs]
    return {
        "median": round(statistics.median(walls), 3),
        "min": min(walls),
        "max": max(walls),
        "user_median": round(statistics.median(r["user"] for r in runs), 3),
        "sys_median": round(statistics.median(r["sys"] for r in runs), 3),
        "peak_rss_mb": max(r["rss_mb"] for r in runs),
    }


def markdown(result):
    lines = [f"{result['program']} {result['n']}, {result['runs']} runs, "
             f"{result['env']['cores']} CPUs, {result['env']['cpu']}", ""]
    for table in result["tables"]:
        if len(result["tables"]) > 1:
            lines += [f"### {TABLE_TITLES.get(table['name'], table['name'])}", ""]
        lines += [
            "| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB "
            "| Size, KB | Build, s | Output |",
            "| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | --- |",
        ]
        for v in table["variants"]:
            lines.append(
                f"| {v['label']} | {v['median']:.2f} | {v['min']:.2f}–{v['max']:.2f} "
                f"| {v['user_median']:.2f} | {v['sys_median']:.2f} | {v['peak_rss_mb']:.0f} "
                f"| {v.get('size_kb', '—')} | {v.get('build_s', '—')} | {v['output']} |")
        lines.append("")
    probe = result.get("cpu_probe")
    if probe:
        lines.append(
            f"{probe['label']}, {probe['n']}: user {probe['all']['user']:.2f} s "
            f"({probe['all']['wall']:.2f} s wall) on {result['env']['cores']} CPUs, "
            f"user {probe['one']['user']:.2f} s ({probe['one']['wall']:.2f} s wall) "
            f"under taskset -c 0")
    return "\n".join(lines).rstrip() + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("program", choices=PROGRAMS)
    parser.add_argument("n", nargs="?", type=int)
    parser.add_argument("runs", nargs="?", type=int, default=10)
    parser.add_argument("--profiles", action="store_true",
                        help="Go-варіанти — профілі збірки owngo (P-NEW-01)")
    parser.add_argument("--json", type=Path, help="куди записати JSON")
    args = parser.parse_args()
    spec = PROGRAMS[args.program]
    n = args.n or spec["n"]

    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        tables = build(spec, work, PROFILE_VARIANTS if args.profiles else GO_VARIANTS)
        checks = {t["name"]: expected_check(t["pkg"], n) for t in tables}
        for t in tables:
            for v in t["variants"]:
                v["runs"] = []
        out = work / "out"
        rusage = work / "rusage"
        stdin = fasta_input(work, n) if spec.get("stdin") else None
        # Програми зі stdin беруть аргумент 0, як на сторінках Benchmarks Game.
        arg = "0" if stdin else str(n)
        for _ in range(args.runs):
            for t in tables:
                for v in t["variants"]:
                    v["runs"].append(m := measure(rusage, [str(v["exe"]), *v.get("args", []), arg], out, stdin))
                    check = checks[t["name"]]
                    if m["exit"] != 0:
                        m["output"] = f"exit {m['exit']}"
                    elif check is None:
                        m["output"] = "no expected"
                    else:
                        m["output"] = "matches" if check(out) else "DIFFERS"

        probe = None
        if "cpu_probe" in spec:
            name, key, probe_n = spec["cpu_probe"]
            table = next(t for t in tables if t["name"] == name)
            v = next(v for v in table["variants"] if v["key"] == key)
            probe = {
                "label": v["label"],
                "n": probe_n,
                "all": measure(rusage, [str(v["exe"]), str(probe_n)], os.devnull),
                "one": measure(rusage, ["taskset", "-c", "0", str(v["exe"]), str(probe_n)],
                               os.devnull),
            }

    ok = True
    for t in tables:
        for v in t["variants"]:
            outputs = {r["output"] for r in v["runs"]}
            v["output"] = outputs.pop() if len(outputs) == 1 else "DIFFERS"
            ok &= v["output"] in ("matches", "no expected")
            v.update(summarize(v["runs"]))
            v["exe"] = v["exe"].name
    result = {
        "program": args.program,
        "n": n,
        "runs": args.runs,
        "profiles": args.profiles,
        "env": environment(),
        "tables": tables,
    }
    if probe:
        result["cpu_probe"] = probe
    if args.json:
        args.json.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
    sys.stdout.write(markdown(result))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
