<script setup lang="ts">
import { CopilotKitProvider } from "@copilotkit/vue/v2";
import { RouterView } from "vue-router";
import { meshAgent } from "./meshAgent";

const agents = { default: meshAgent };
</script>

<template>
  <CopilotKitProvider
    :self-managed-agents="agents"
    :enable-inspector="false"
    :show-dev-console="false"
    credentials="same-origin"
  >
    <div class="mesh-root" data-copilotkit>
      <RouterView />
    </div>
  </CopilotKitProvider>
</template>
