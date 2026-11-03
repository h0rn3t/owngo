#!/usr/bin/env python3
"""Єдиний набір заміру треку швидкодії (S1.1).

Збирає програму корпусу в кожному варіанті — go, llgo без тега, llgo -tags
owngo з типовим -Os і з -O2 -lto full, Rust — і запускає кожен варіант RUNS
разів, чергуючи варіанти в кожному колі. Вивід кожного прогону звіряється з
testdata/expected-N.* Go-програми таблиці. Таблиці Markdown — у stdout, JSON з
усіма прогонами й оточенням — у файл --json.

binary-trees має дві таблиці: звичайні алокації (Go #2) і арени
(binarytreesarena, Rust #5). Висновок між таблицями не робиться. Для Go #2 під
owngo окремо міряється CPU-час на 18: на всіх ядрах і під taskset -c 0 (S1).

Лише Linux: ru_maxrss читається як КБ. Потрібні Go, LLVM 23 першим у PATH
(clang збирає rusage.c), cargo, taskset.

Використання: owngo/bench/compare.py PROGRAM [N] [RUNS] [--json FILE]
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
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BENCH = ROOT / "owngo" / "bench"
X86 = platform.machine() == "x86_64"

# Таблиці програми: назва, Go-пакет у owngo/bench, крейт Rust (чи None) і
# підпис Rust. Підписи Go-варіантів однакові для всіх програм.
PROGRAMS = {
    "binarytrees": {
        "n": 21,
        "tables": [
            ("regular", "binarytrees", None, None),
            ("arena", "binarytreesarena", "binarytreesrust", "Rust #5, bumpalo + rayon"),
        ],
        # CPU-час Go #2 під owngo на 18 (S1): таблиця, варіант, N.
        "cpu_probe": ("regular", "owngo", 18),
    },
    "fannkuchredux": {
        "n": 12,
        "tables": [("main", "fannkuchredux", "fannkuchreduxrust", "Rust #6, rayon")],
    },
    "mandelbrot": {
        "n": 16000,
        "tables": [("main", "mandelbrot", "mandelbrotrust", "Rust #4, rayon")],
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
    ("owngo-lto", "llgo -tags owngo -O2 -lto full", ["-tags", "owngo", "-O2", "-lto", "full"]),
]

# Прапори сторінок Benchmarks Game: Go — GOAMD64=v2, Rust — release з
# codegen-units=1 (Cargo.toml) і target-cpu=ivybridge.
GO_ENV = {"GOAMD64": "v2"} if X86 else {}
RUST_ENV = {"RUSTFLAGS": "-C target-cpu=ivybridge"} if X86 else {}

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


def build(spec, work):
    """Збирає всі варіанти всіх таблиць; повертає таблиці з шляхами бінарників."""
    run(["clang", "-O2", "-o", str(work / "rusage"), str(BENCH / "rusage.c")])
    llgo = work / "llgo"
    run(["go", "build", "-o", str(llgo), "./cmd/llgo"])
    tables = []
    for name, pkg, crate, rust_label in spec["tables"]:
        variants = []
        for key, label, flags in GO_VARIANTS:
            exe = work / f"{pkg}-{key}"
            if flags is None:
                run(["go", "build", "-o", str(exe), f"./owngo/bench/{pkg}"], env=GO_ENV)
            else:
                run([str(llgo), "build", *flags, "-o", str(exe), f"./owngo/bench/{pkg}"],
                    env={"LLGO_ROOT": str(ROOT)})
            variants.append({"key": key, "label": f"{pkg}, {label}", "exe": exe})
        if crate:
            target = work / "cargo"
            run(["cargo", "build", "--quiet", "--release", "--locked",
                 "--manifest-path", str(BENCH / crate / "Cargo.toml"),
                 "--target-dir", str(target)], env=RUST_ENV)
            exe = work / crate
            shutil.copy(target / "release" / crate.removesuffix("rust"), exe)
            variants.append({"key": "rust", "label": rust_label, "exe": exe})
        tables.append({"name": name, "pkg": pkg, "variants": variants})
    return tables


def measure(rusage, cmd, out):
    """Запускає cmd з stdout у файл out через rusage.c; повертає час, CPU і
    пікову RSS."""
    line = subprocess.run([str(rusage), str(out), *cmd], capture_output=True,
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
            "| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB | Output |",
            "| --- | ---: | --- | ---: | ---: | ---: | --- |",
        ]
        for v in table["variants"]:
            lines.append(
                f"| {v['label']} | {v['median']:.2f} | {v['min']:.2f}–{v['max']:.2f} "
                f"| {v['user_median']:.2f} | {v['sys_median']:.2f} | {v['peak_rss_mb']:.0f} "
                f"| {v['output']} |")
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
    parser.add_argument("--json", type=Path, help="куди записати JSON")
    args = parser.parse_args()
    spec = PROGRAMS[args.program]
    n = args.n or spec["n"]

    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        tables = build(spec, work)
        checks = {t["name"]: expected_check(t["pkg"], n) for t in tables}
        for t in tables:
            for v in t["variants"]:
                v["runs"] = []
        out = work / "out"
        rusage = work / "rusage"
        for _ in range(args.runs):
            for t in tables:
                for v in t["variants"]:
                    v["runs"].append(m := measure(rusage, [str(v["exe"]), str(n)], out))
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
