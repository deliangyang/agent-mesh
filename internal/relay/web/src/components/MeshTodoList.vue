<script setup lang="ts">
import { computed } from "vue";
import { formatTodoTitle, todoLabel, type TodoItem } from "../todos.ts";

const props = defineProps<{
  items: TodoItem[];
  pending?: boolean;
}>();

const visible = computed(() => props.items.filter((item) => todoLabel(item)));
const title = computed(() => formatTodoTitle(visible.value));
const show = computed(() => visible.value.length > 0 || !!props.pending);

function statusLabel(status: TodoItem["status"]): string {
  if (status === "completed") return "已完成";
  if (status === "in_progress") return "进行中";
  if (status === "cancelled") return "已取消";
  return "未开始";
}
</script>

<template>
  <div v-if="show" class="mesh-todo" role="list" :aria-label="title">
    <div class="mesh-todo-head">{{ visible.length ? title : "正在更新待办" }}</div>
    <div
      v-for="item in visible"
      :key="item.id"
      class="mesh-todo-item"
      :class="'is-' + item.status"
      role="listitem"
      :aria-label="todoLabel(item) + '，' + statusLabel(item.status)"
    >
      <span class="mesh-todo-mark" aria-hidden="true">
        <svg v-if="item.status === 'completed'" viewBox="0 0 12 12" fill="none">
          <rect x="0.7" y="0.7" width="10.6" height="10.6" rx="2" stroke="currentColor" stroke-width="1.3" />
          <path
            d="M3 6.2 5.1 8.3 9 3.8"
            stroke="currentColor"
            stroke-width="1.4"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
        <svg v-else-if="item.status === 'cancelled'" viewBox="0 0 12 12" fill="none">
          <rect x="0.7" y="0.7" width="10.6" height="10.6" rx="2" stroke="currentColor" stroke-width="1.3" />
          <path d="M4 4l4 4M8 4l-4 4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
        </svg>
        <svg v-else viewBox="0 0 12 12" fill="none">
          <rect x="0.7" y="0.7" width="10.6" height="10.6" rx="2" stroke="currentColor" stroke-width="1.3" />
        </svg>
      </span>
      <span class="mesh-todo-text">{{ todoLabel(item) }}</span>
    </div>
  </div>
</template>
