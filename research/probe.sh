#!/usr/bin/env bash
# Runs the same requests against each variant and records responses, specs, and tsc results.
set -u
cd "$(dirname "$0")"
base=http://localhost:8080

probe() {
  local name=$1 spec=$2 docs=$3
  local out=results/$name
  rm -rf "$out"; mkdir -p "$out"
  (cd "$name" && go build -o "../bin/$name-app.exe" .) || { echo "$name: build failed"; return; }
  "./bin/$name-app.exe" >"$out/server.log" 2>&1 &
  for _ in $(seq 50); do curl -s -o /dev/null "$base/pets" && break; sleep 0.2; done

  req() { # label method path [body]
    local label=$1 method=$2 path=$3 body=${4:-}
    local args=(-s -o "$out/$label.json" -w "%{http_code}" -X "$method" "$base$path")
    [ -n "$body" ] && args+=(-H "Content-Type: application/json" -d "$body")
    printf '%-22s %s %-24s -> %s\n' "$label" "$method" "$path" "$(curl "${args[@]}")" | tee -a "$out/status.txt"
  }
  req create-ok        POST /pets '{"name":"Milo","kind":"cat"}'
  req create-invalid   POST /pets '{"name":"","kind":"fish"}'
  req create-missing   POST /pets '{"name":"Milo"}'
  req create-malformed POST /pets '{"name":'
  req get-ok           GET  /pets/1
  req get-404          GET  /pets/999
  req get-bad-id       GET  /pets/abc
  req list-filter      GET  '/pets?kind=cat'
  req list-bad-limit   GET  '/pets?limit=500'
  req update-ok        PUT  /pets/1 '{"name":"Mila","kind":"cat"}'
  if [ "$spec" = static ]; then :; else curl -s "$base$spec" -o "$out/openapi.json"; fi
  [ -n "$docs" ] && printf '%-22s %s %-24s -> %s\n' docs-page GET "$docs" "$(curl -s -o /dev/null -w '%{http_code}' "$base$docs")" | tee -a "$out/status.txt"
  taskkill //F //IM "$name-app.exe" >/dev/null 2>&1
  sleep 0.5
}

mkdir -p bin results
echo "== gin-swaggo";   probe gin-swaggo   static /docs/index.html
echo "== oapi-codegen"; probe oapi-codegen static ""
echo "== huma";         probe huma         /openapi.json /docs
echo "== tack";         probe tack         /openapi.json /docs

cp gin-swaggo/docs/swagger.json results/gin-swaggo/swagger.json
cp oapi-codegen/openapi.yaml results/oapi-codegen/openapi.yaml

echo "== TypeScript"
npx openapi-typescript oapi-codegen/openapi.yaml -o oapi-codegen/ts/api.d.ts >/dev/null && echo "oapi-codegen: types generated"
npx openapi-typescript results/huma/openapi.json -o huma/ts/api.d.ts >/dev/null && echo "huma: types generated"
npx openapi-typescript results/tack/openapi.json -o tack/ts/api.d.ts >/dev/null && echo "tack: types generated"
for d in gin-swaggo oapi-codegen huma tack; do
  if (cd "$d/ts" && npx tsc -p . >"../../results/$d/tsc.txt" 2>&1); then echo "$d: tsc OK"; else echo "$d: tsc FAILED"; sed -n '1,5p' "results/$d/tsc.txt"; fi
done
