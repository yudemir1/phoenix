# Reads `go test -v` output and prints each subtest (t.Run) on one line
# with a green-background checkmark (pass) or red-background cross (fail),
# describing what it checks. Build/parse errors are passed through as-is
# so failures are never hidden.

BEGIN {
	pass = 0
	fail = 0
	GREEN_BG = "\033[30;42m"
	RED_BG   = "\033[97;41m"
	RESET    = "\033[0m"
}

# --- PASS: TestX/subtest_name (0.00s)
$1 == "---" && $2 == "PASS:" && index($3, "/") > 0 {
	n = split($3, parts, "/")
	desc = parts[n]
	gsub(/_/, " ", desc)
	printf "%s ✅ %s%s %s\n", GREEN_BG, RESET, "", desc
	pass++
	next
}

# --- FAIL: TestX/subtest_name (0.00s)
$1 == "---" && $2 == "FAIL:" && index($3, "/") > 0 {
	n = split($3, parts, "/")
	desc = parts[n]
	gsub(/_/, " ", desc)
	printf "%s ❌ %s%s %s\n", RED_BG, RESET, "", desc
	fail++
	next
}

# Top-level TestX lines (wrappers with no subtest) are skipped; everything
# else (compile errors, "ok"/"FAIL" package summaries, etc.) passes through
$1 == "---" && ($2 == "PASS:" || $2 == "FAIL:") { next }
/^=== RUN|^=== PAUSE|^=== CONT/ { next }
/^PASS$/ { next }
{ print }

END {
	print ""
	total = pass + fail
	if (fail == 0) {
		printf "%s ✅ %sAll tests passed: %d/%d\n", GREEN_BG, RESET, pass, total
	} else {
		printf "%s ❌ %s%d failed, %d passed (total %d)\n", RED_BG, RESET, fail, pass, total
	}
}
