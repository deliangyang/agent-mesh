<script setup lang="ts">
defineProps<{
  streaming?: boolean;
}>();
</script>

<template>
  <div
    class="mesh-think-line"
    :data-streaming="streaming ? 'true' : undefined"
    role="status"
    aria-live="polite"
  >
    <svg class="mesh-think-icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="5.2" stroke="currentColor" stroke-width="1.4" opacity="0.28" />
      <path
        d="M8 2.8a5.2 5.2 0 0 1 5.2 5.2"
        stroke="currentColor"
        stroke-width="1.4"
        stroke-linecap="round"
      />
    </svg>
    <span>{{ streaming ? "思考中" : "已思考" }}</span>
  </div>
</template>
