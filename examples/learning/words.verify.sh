set -eu
node <<'JS'
const assert = require('node:assert/strict');
const { uniqueWords } = require(process.cwd() + '/utilities.js');
for (const [input, expected] of [
  [[], []], [[' A ', 'a', '', 'B', ' b '], ['A', 'B']],
  [['\t', '\n', '  '], []], [['Zero', 'zero', ' one '], ['Zero', 'one']],
]) {
  const before = [...input];
  assert.deepEqual(uniqueWords(input), expected);
  assert.deepEqual(input, before);
}
console.log('Passed independent uniqueWords checks.');
JS
