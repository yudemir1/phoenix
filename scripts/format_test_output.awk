# Reads `go test -v` output and prints each *leaf* test (a subtest added
# via t.Run, or a top-level test that has no subtests at all) on one line
# with a green-background checkmark (pass) or red-background cross (fail),
# describing what it checks. Build/parse errors are passed through as-is
# so failures are never hidden.
#
# go test -v prints a parent test's own rollup line ("--- PASS: TestX")
# before its subtests' lines ("--- PASS: TestX/sub"), so we can't decide
# whether to suppress a parent line in a single streaming pass. Instead the
# whole output is buffered and processed in two passes in END: pass 1 finds
# every parent name that has at least one subtest; pass 2 prints each line,
# skipping a parent's own rollup line when it has subtests (they're printed
# individually instead) and printing it plainly when it doesn't (it was a
# leaf test all along).

BEGIN {
	pass = 0
	fail = 0
	GREEN_BG = "\033[30;42m"
	RED_BG   = "\033[97;41m"
	RESET    = "\033[0m"
}

{ lines[NR] = $0; last = NR }

function describe(name,    n, parts, d) {
	n = split(name, parts, "/")
	d = parts[n]
	gsub(/_/, " ", d)
	return d
}

END {
	for (i = 1; i <= last; i++) {
		n = split(lines[i], f)
		if (f[1] == "---" && (f[2] == "PASS:" || f[2] == "FAIL:")) {
			slash = index(f[3], "/")
			if (slash > 0) {
				has_children[substr(f[3], 1, slash - 1)] = 1
			}
		}
	}

	for (i = 1; i <= last; i++) {
		line = lines[i]
		n = split(line, f)
		if (f[1] == "---" && (f[2] == "PASS:" || f[2] == "FAIL:")) {
			name = f[3]
			if (index(name, "/") == 0 && has_children[name]) {
				continue # redundant parent rollup; its subtests are printed individually
			}
			desc = describe(name)
			if (f[2] == "PASS:") {
				printf "%s ✅ %s %s\n", GREEN_BG, RESET, desc
				pass++
			} else {
				printf "%s ❌ %s %s\n", RED_BG, RESET, desc
				fail++
			}
			continue
		}
		if (line ~ /^=== RUN|^=== PAUSE|^=== CONT/) continue
		if (line == "PASS") continue
		print line
	}

	print ""
	total = pass + fail
	if (fail == 0) {
		printf "%s ✅ %sAll tests passed: %d/%d\n", GREEN_BG, RESET, pass, total
	} else {
		printf "%s ❌ %s%d failed, %d passed (total %d)\n", RED_BG, RESET, fail, pass, total
	}
}
