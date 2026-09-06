set -eu
node <<'JS'
const assert = require('node:assert/strict');
const { clamp } = require(process.cwd() + '/utilities.js');
for (const [v, lo, hi, expected] of [[5,0,10,5],[-1,0,10,0],[11,0,10,10],[-2,-5,-1,-2],[5,3,3,3]]) {
  assert.equal(clamp(v,lo,hi),expected);
}
assert.throws(() => clamp(2,5,1), RangeError);
for (const args of [[NaN,0,1],[0,NaN,1],[0,0,NaN],['1',0,2],[0,null,2]]) {
  assert.throws(() => clamp(...args), TypeError);
}
console.log('Passed independent clamp checks.');
JS
