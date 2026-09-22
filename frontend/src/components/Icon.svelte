<script lang="ts">
  export let name: string
  export let size = 16

  const paths: Record<string, string> = {
    plus: 'M8 3v10M3 8h10',
    refresh: 'M13 8a5 5 0 1 1-1.5-3.5M13 2.5V5h-2.5',
    download: 'M8 2.5v8m0 0L5 7.5m3 3 3-3M3 13.5h10',
    upload: 'M8 13.5v-8m0 0L5 8.5m3-3 3 3M3 2.5h10',
    'chevron-right': 'M6 4l4 4-4 4',
    'chevron-down': 'M4 6l4 4 4-4',
    settings: 'M8 10a2 2 0 1 0 0-4 2 2 0 0 0 0 4ZM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4',
    tag: 'M2.5 2.5h5l6 6-5 5-6-6v-5ZM5.5 5.5h.01',
    cloud: 'M4.5 12.5h7a3 3 0 0 0 .4-6 4 4 0 0 0-7.7 1 2.5 2.5 0 0 0 .3 5Z',
    check: 'M3.5 8.5l3 3 6-7',
    'panel-right': 'M2.5 3.5h11v9h-11zM10 3.5v9',
    terminal: 'M2.5 3.5h11v9h-11zM5 6.5l2 1.5-2 1.5M8.5 10h2.5',
    search: 'M7 12a5 5 0 1 0 0-10 5 5 0 0 0 0 10ZM10.5 10.5 14 14',
    x: 'M4 4l8 8M12 4l-8 8',
    send: 'M8 13V3M4 7l4-4 4 4',
    sparkle: 'M8 2l1.5 4.5L14 8l-4.5 1.5L8 14l-1.5-4.5L2 8l4.5-1.5z',
    copy: 'M5.5 5.5h7v8h-7zM3.5 10.5v-8h7',
    stop: 'M4.5 4.5h7v7h-7z',
  }
</script>

<svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  <path d={paths[name] ?? ''} />
</svg>
