<script setup lang="ts">
import { computed, ref } from "vue";
import { catalog } from "../catalog";
import { t } from "../locale";
import { loadModelPrefs, modelName, modelsForEngine, saveModelPref } from "../modelPrefs";
import type { ModelCard } from "../types";

type EngineRow = {
  engine: string;
  title: string;
  models: ModelCard[];
  model: string;
  thinking: string;
  levels: string[];
};

const prefs = ref(loadModelPrefs());

function rowOf(engine: string): EngineRow | undefined {
  const pref = prefs.value[engine] || {};
  const models = modelsForEngine(catalog.models, engine);
  const hit = (pref.model && models.find((m) => modelName(m.id, engine) === pref.model)) || models[0];
  const levels = (hit?.thinking || []).slice();
  return {
    engine,
    title: engine,
    models,
    model: hit ? modelName(hit.id, engine) : "",
    thinking: pref.thinking && levels.includes(pref.thinking) ? pref.thinking : "",
    levels,
  };
}

const rows = computed<EngineRow[]>(() => {
  const engines: string[] = [];
  for (const e of catalog.engines) {
    if (e.name && !engines.includes(e.name)) {
      engines.push(e.name);
    }
  }
  return engines.map((engine) => {
    const row = rowOf(engine);
    const card = catalog.engines.find((e) => e.name === engine);
    return { ...row, title: card?.title || engine };
  });
});

function pickModel(engine: string, name: string): void {
  const row = rowOf(engine);
  const hit = row?.models.find((m) => modelName(m.id, engine) === name);
  const levels = hit?.thinking || [];
  const keep = levels.includes(row?.thinking || "") ? row?.thinking : "";
  saveModelPref(engine, { model: name, thinking: keep || "" });
  prefs.value = loadModelPrefs();
}

function pickThinking(engine: string, level: string): void {
  const row = rowOf(engine);
  saveModelPref(engine, { model: row?.model || "", thinking: level });
  prefs.value = loadModelPrefs();
}
</script>

<template>
  <section class="mesh-settings-panel" :aria-label="t('settings.navModels')">
    <h2>{{ t("settings.navModels") }}</h2>
    <p class="mesh-settings-hint">{{ t("settings.modelsHint") }}</p>
    <div v-if="!rows.length" class="mesh-settings-empty">{{ t("settings.modelsEmpty") }}</div>
    <div v-for="row in rows" :key="row.engine" class="mesh-model-row">
      <div class="mesh-model-engine">
        <strong>{{ row.title }}</strong>
        <span v-if="row.title !== row.engine">{{ row.engine }}</span>
      </div>
      <label class="mesh-model-field">
        <span>{{ t("settings.modelsModel") }}</span>
        <select :value="row.model" @change="pickModel(row.engine, ($event.target as HTMLSelectElement).value)">
          <option v-for="m in row.models" :key="m.id" :value="modelName(m.id, row.engine)">
            {{ modelName(m.id, row.engine) }}
          </option>
        </select>
      </label>
      <label v-if="row.levels.length" class="mesh-model-field">
        <span>{{ t("settings.modelsThinking") }}</span>
        <select :value="row.thinking" @change="pickThinking(row.engine, ($event.target as HTMLSelectElement).value)">
          <option value="">{{ t("settings.modelsThinkingOff") }}</option>
          <option v-for="level in row.levels" :key="level" :value="level">{{ level }}</option>
        </select>
      </label>
      <div v-else class="mesh-model-nothink">{{ t("settings.modelsNoThinking") }}</div>
    </div>
  </section>
</template>
