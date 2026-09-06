set -eu
node <<'JS'
const assert = require('node:assert/strict');
const { finiteSum } = require(process.cwd() + '/utilities.js');
for (const [input, expected] of [
  [[], 0], [[1, 2, 3], 6], [[1, '2', null, undefined, true, NaN, Infinity, -Infinity, 4], 5],
  [[-3, 2.5, 0.5], 0], [['3', false], 0],
]) {
  const before = [...input];
  assert.equal(finiteSum(input), expected);
  assert.deepEqual(input, before);
}
console.log('Passed independent finiteSum checks.');
JS
