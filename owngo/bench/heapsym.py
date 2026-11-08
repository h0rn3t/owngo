#!/usr/bin/env python3
"""Зводить дамп семплера купи (тег owngoheap) у живі байти на стек.

    heapsym.py heap.txt [-n REQUESTS] [-top N] [-depth D]

Живі байти стека — (виділені − звільнені) семпли × rate. Стек
показується з першого кадру поза алокатором рантайму, D кадрів (типово 3);
з -n — ще й на запит.
"""
import argparse
import collections
import subprocess

# Кадри алокатора, які пропускаються до першого кадру місця.
ALLOC_FRAMES = ("owngo_malloc", "AllocU", "AllocZ", "AllocRoot", "AllocCounted",
                "MakeSlice", "GrowSlice", "NewString", "StringCat")


def is_exec(obj, cache={}):
    if obj not in cache:
        with open(obj, "rb") as f:
            cache[obj] = f.read(18)[16] == 2  # e_type == ET_EXEC
    return cache[obj]


def read(path):
    maps, base, sites, rate = [], {}, [], 1
    for line in open(path):
        kind, rest = line.split(" ", 1)
        if kind == "map":
            f = rest.split()
            if len(f) < 6 or not f[5].startswith("/"):
                continue
            if int(f[2], 16) == 0:
                base.setdefault(f[5], int(f[0].split("-")[0], 16))
            if "x" in f[1]:
                lo, hi = (int(x, 16) for x in f[0].split("-"))
                maps.append((lo, hi, f[5]))
        elif kind == "rate":
            rate = int(rest)
        else:
            f = rest.split()
            sites.append(([int(x) for x in f[:4]], [int(x, 16) for x in f[4:]]))
    return maps, base, sites, rate


def symbolize(maps, base, pcs):
    by_obj = collections.defaultdict(list)
    for pc in pcs:
        for lo, hi, obj in maps:
            if lo <= pc < hi:
                by_obj[obj].append(pc)
                break
    names = {}
    for obj, objpcs in by_obj.items():
        # Адреса повернення − 1 — інструкція виклику; у PIE і .so — зсув від бази.
        offs = [pc - 1 - (0 if is_exec(obj) else base[obj]) for pc in objpcs]
        out = subprocess.run(
            ["llvm-symbolizer", "--obj=" + obj, "--functions=linkage",
             "--no-inlines", "--output-style=GNU"],
            input="".join("0x%x\n" % o for o in offs),
            capture_output=True, text=True).stdout.split("\n")
        for i, pc in enumerate(objpcs):
            loc = out[2 * i + 1].split(":")
            names[pc] = "%s@%s:%s" % (out[2 * i], loc[0].rsplit("/", 1)[-1], loc[1] if len(loc) > 1 else "?")
    return names


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("path")
    ap.add_argument("-n", type=float, default=0, help="запитів, для колонок на запит")
    ap.add_argument("-top", type=int, default=30)
    ap.add_argument("-depth", type=int, default=3)
    a = ap.parse_args()
    maps, base, sites, rate = read(a.path)
    names = symbolize(maps, base, {pc for _, st in sites for pc in st})
    live, objs = collections.Counter(), collections.Counter()
    for (allocs, nbytes, frees, freed), st in sites:
        frames = [names.get(pc, "??") for pc in st]
        i = 0
        while i < len(frames) - 1 and frames[i].split("@")[0].rsplit(".", 1)[-1].startswith(ALLOC_FRAMES):
            i += 1
        key = " <- ".join(frames[i:i + a.depth])
        live[key] += (nbytes - freed) * rate
        objs[key] += (allocs - frees) * rate
    total = sum(live.values())
    div = a.n or 1
    unit = "B/req" if a.n else "B"
    print("live %.1f MiB%s" % (total / 2**20, ", %.0f B/req" % (total / a.n) if a.n else ""))
    for key, b in live.most_common(a.top):
        print("%10.0f %s %8.1f obj  %s" % (b / div, unit, objs[key] / div, key))


if __name__ == "__main__":
    main()
