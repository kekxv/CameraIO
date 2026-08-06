import { createRouter, createWebHistory } from 'vue-router'
import { isLoggedIn } from '../api'

const routes = [
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/Login.vue'),
    meta: { guest: true },
  },
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', redirect: '/cameras' },
      {
        path: 'cameras',
        name: 'cameras',
        component: () => import('../views/Cameras.vue'),
      },
      {
        path: 'live',
        name: 'live',
        component: () => import('../views/Live.vue'),
      },
      {
        path: 'recordings',
        name: 'recordings',
        component: () => import('../views/Recordings.vue'),
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to) => {
  if (to.meta.requiresAuth && !isLoggedIn()) {
    return { name: 'login' }
  }
  if (to.meta.guest && isLoggedIn()) {
    return { name: 'cameras' }
  }
})

export default router
