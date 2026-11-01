<script setup lang="ts">
import { dismissToast, toasts } from "../toast";
</script>

<template>
  <div class="mesh-toasts" aria-live="polite" aria-relevant="additions">
    <button
      v-for="t in toasts"
      :key="t.id"
      type="button"
      class="mesh-toast"
      :data-kind="t.kind"
      @click="dismissToast(t.id)"
    >
      {{ t.text }}
    </button>
  </div>
</template>
