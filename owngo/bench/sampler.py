#!/usr/bin/env python3
"""Зводить вивід sampler.c у частки функцій: sampler.py out.txt [top]."""
import collections
import subprocess
import sys


def is_exec(obj, cache={}):
    if obj not in cache:
        with open(obj, "rb") as f:
            cache[obj] = f.read(18)[16] == 2  # e_type == ET_EXEC
    return cache[obj]


def main():
    path = sys.argv[1]
    top = int(sys.argv[2]) if len(sys.argv) > 2 else 15
    maps, pcs, base = [], [], {}
    for line in open(path):
        kind, rest = line.split(" ", 1)
        if kind == "map":
            f = rest.split()
            # [vdso] і подібні — не файли; їхні адреси йдуть у «no mapping».
            if len(f) < 6 or not f[5].startswith("/"):
                continue
            if int(f[2], 16) == 0:
                base.setdefault(f[5], int(f[0].split("-")[0], 16))
            if "x" not in f[1]:
                continue
            lo, hi = (int(x, 16) for x in f[0].split("-"))
            maps.append((lo, hi, int(f[2], 16), f[5]))
        else:
            pcs.append(int(rest, 16))
    by_obj = collections.defaultdict(list)
    unknown = 0
    for pc in pcs:
        for lo, hi, _, obj in maps:
            if lo <= pc < hi:
                # Адреса для llvm-symbolizer — віртуальна адреса ELF. У PIE і
                # .so перший сегмент має адресу 0, у ET_EXEC — справжню.
                by_obj[obj].append(pc - (0 if is_exec(obj) else base[obj]))
                break
        else:
            unknown += 1
    funcs = collections.Counter()
    for obj, offs in by_obj.items():
        uniq = sorted(set(offs))
        inp = "".join("0x%x\n" % o for o in uniq)
        out = subprocess.run(
            ["llvm-symbolizer", "--obj=" + obj, "--functions=linkage",
             "--no-inlines", "--output-style=GNU"],
            input=inp, capture_output=True, text=True).stdout.split("\n")
        names = {o: out[2 * i] for i, o in enumerate(uniq)}
        short = obj.rsplit("/", 1)[-1]
        for o in offs:
            name = names[o]
            if name == "??":
                name = "?? (%s)" % short
            funcs[name] += 1
    if unknown:
        funcs["?? (no mapping)"] += unknown
    total = len(pcs)
    print("samples %d" % total)
    for name, n in funcs.most_common(top):
        print("%6.1f%% %7d  %s" % (100.0 * n / total, n, name))


if __name__ == "__main__":
    main()
