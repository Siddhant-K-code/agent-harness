// Each task fixes one export while preserving the others.
function uniqueWords(words) { return [...new Set(words)]; }
function finiteSum(values) { return values.reduce((sum, value) => sum + value, 0); }
function clamp(value, min, max) { return Math.max(min, value); }
module.exports = { uniqueWords, finiteSum, clamp };
