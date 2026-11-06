<template>
  <div class="markdown-body" v-html="rendered"></div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import MarkdownIt from 'markdown-it'

const props = defineProps<{ content: string }>()

// html:false -> 原始 HTML 一律转义, 防止模型/用户内容注入脚本
const md = new MarkdownIt({ html: false, linkify: true, breaks: true })

const rendered = computed(() => md.render(props.content || ''))
</script>
