<script setup lang="ts">
defineProps<{
  mode?: "think" | "live";
}>();
</script>

<template>
  <div
    class="mesh-think-line"
    :data-mode="mode || 'think'"
    role="status"
    aria-live="polite"
    :aria-label="mode === 'live' ? '进行中' : '思考中'"
  >
    <span v-if="mode === 'live'" class="mesh-lamp" aria-hidden="true"></span>
    <template v-else>
      <svg class="mesh-think-icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <circle cx="8" cy="8" r="5.2" stroke="currentColor" stroke-width="1.4" opacity="0.28" />
        <path
          d="M8 2.8a5.2 5.2 0 0 1 5.2 5.2"
          stroke="currentColor"
          stroke-width="1.4"
          stroke-linecap="round"
        />
      </svg>
      <span>思考中</span>
    </template>
  </div>
</template>
