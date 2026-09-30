#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
git init -q --bare "$fixture/origin.git"
git init -q -b main "$fixture/work"
cd "$fixture/work"
git config user.email ci@td.invalid
git config user.name 'td release guard test'
git remote add origin "$fixture/origin.git"
printf '## [v1.2.3] - 2026-09-30\n' > CHANGELOG.md
git add CHANGELOG.md
git commit -qm 'release fixture'
git tag -a v1.2.3 -m v1.2.3
git push -q origin main --tags

# GitHub is the only external boundary; exercise real git refs and commits.
mkdir "$fixture/bin"
cat > "$fixture/bin/gh" <<'EOF'
#!/usr/bin/env bash
if [[ ${TEST_CI_RESULT:-success} == unavailable ]]; then exit 1; fi
case ${TEST_CI_RESULT:-success} in
  missing) echo '[]' ;;
  pending) echo '[{"status":"in_progress","conclusion":""}]' ;;
  failure) echo '[{"status":"completed","conclusion":"failure"}]' ;;
  *) echo '[{"status":"completed","conclusion":"success"}]' ;;
esac
EOF
chmod +x "$fixture/bin/gh"
export PATH="$fixture/bin:$PATH" RELEASE_VERSION=v1.2.3

reject() {
  if "$script_dir/check-release-tag.sh" >"$fixture/output" 2>&1; then
    echo "FAIL: accepted $1" >&2
    exit 1
  fi
  echo "PASS: rejected $1"
}

"$script_dir/check-release-tag.sh"
for result in missing pending failure unavailable; do
  export TEST_CI_RESULT=$result
  reject "$result CI"
done
unset TEST_CI_RESULT
RELEASE_VERSION=v01.2.3 reject 'invalid version'
git tag v1.2.4
RELEASE_VERSION=v1.2.4 reject 'missing changelog'
git commit --allow-empty -qm 'unpublished change'
reject 'checkout differing from tag'
git push -q origin main
git checkout -q v1.2.3
reject 'tag behind live main'
echo 'Release publication guard tests passed'
