# Design reference (ResearchSphere UI Screens, Claude Design)

Reference only. Not served by the app, not imported by code.

- screens/<Name>.html   desktop artboard (1280 px wide)
- screens/<Name>_m.html mobile artboard (390 px wide)
- screens/Main.html     FR coverage map, not a screen

Each file is plain HTML + CSS (inline styles plus a <style> block in
<helmet>). {{theme}}, <x-dc>, <sc-if> and support.js are design-tool
markup: ignore them. Images (/_blob/...) are placeholders.
Use light theme only.
