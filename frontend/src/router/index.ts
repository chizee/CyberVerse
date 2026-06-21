import { createRouter, createWebHistory } from 'vue-router'
import { loadLaunchWorkspaceMode } from '../utils/launchModePreference'
import LaunchConfigPage from '../pages/LaunchConfigPage.vue'
import OfflineVideoPage from '../pages/OfflineVideoPage.vue'

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior(_to, _from, savedPosition) {
    if (savedPosition) {
      return savedPosition
    }

    return { left: 0, top: 0 }
  },
  routes: [
    {
      path: '/',
      name: 'landing',
      component: () => import('../pages/LandingPage.vue'),
    },
    {
      path: '/kanshan',
      name: 'kanshan-landing',
      component: () => import('../pages/KanshanLandingPage.vue'),
    },
    {
      path: '/characters',
      name: 'characters',
      component: () => import('../pages/CharacterListPage.vue'),
    },
    {
      path: '/characters/new',
      name: 'character-create',
      component: () => import('../pages/CharacterEditPage.vue'),
    },
    {
      path: '/characters/:id/edit',
      name: 'character-edit',
      component: () => import('../pages/CharacterEditPage.vue'),
    },
    {
      path: '/launch/:id',
      name: 'launch',
      redirect: to => ({
        name: loadLaunchWorkspaceMode() === 'live' ? 'launch-live' : 'launch-offline',
        params: { id: to.params.id },
      }),
    },
    {
      path: '/launch/:id/offline',
      name: 'launch-offline',
      component: OfflineVideoPage,
    },
    {
      path: '/launch/:id/live',
      name: 'launch-live',
      component: LaunchConfigPage,
    },
    {
      path: '/characters/:id/offline-videos',
      redirect: to => ({ name: 'launch-offline', params: { id: to.params.id } }),
    },
    {
      path: '/session/:id',
      name: 'session',
      component: () => import('../pages/SessionPage.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('../pages/SettingsPage.vue'),
    },
  ],
})

export default router
