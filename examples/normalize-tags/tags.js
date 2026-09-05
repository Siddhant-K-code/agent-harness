function normalizeTags(tags) {
  return [...new Set(tags)];
}

module.exports = { normalizeTags };
