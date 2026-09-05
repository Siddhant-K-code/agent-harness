set -eu
node <<'JS'
const assert = require('node:assert/strict');
const { normalizeTags } = require('/workspace/tags.js');
const cases = [
  [[], []],
  [[' a ', 'b', 'A', ''], ['a', 'b']],
  [['Go', 'go', 'GO'], ['Go']],
  [['   ', '\t', '\n'], []],
  [[' Zoo ', ' apple', 'Banana ', 'zoo'], ['Zoo', 'apple', 'Banana']],
  [['one', ' one two ', 'ONE TWO'], ['one', 'one two']],
];
for (const [input, expected] of cases) {
  const before = [...input];
  assert.deepEqual(normalizeTags(input), expected);
  assert.deepEqual(input, before, 'input must not be mutated');
}
console.log(`Passed ${cases.length} independent cases plus mutation checks.`);
JS
