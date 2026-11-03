import { createRouter, createWebHistory } from "vue-router";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/login", component: () => import("./views/Login.vue") },
    { path: "/", component: () => import("./views/Chat.vue") },
    { path: "/settings/:section?", component: () => import("./views/Settings.vue") },
  ],
});
