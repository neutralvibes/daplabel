# 🚀 Cline Pass Optimization Guide (Best Bang for Your Buck)

Use this cheat sheet to switch models dynamically inside Cline. Matching the model to the exact task preserves your subscription quota and stops expensive "thinking loops."

---

## 🏗️ Phase 1: High-Level System Architecture & Planning
* **Best Model:** `deepseek-v4-pro` (or `kimi-k3` when available)
* **Cline Mode:** ⚠️ MUST BE SET TO **"PLAN MODE"** (Do not let it execute code)
* **Best Use Cases:**
  * Mapping out database schemas or entity-relationship (ER) diagrams.
  * Figuring out microservice communication strategies.
  * Dropping massive corporate engineering docs (up to 1M tokens) to extract an implementation path.
  * Writing technical step-by-step blueprints before any files are written.

---

## ✍️ Phase 2: Writing Brand New Code From Scratch
* **Best Model:** `qwen-3.7-max` (or `qwen-3.7-plus`)
* **Cline Mode:** **"ACT MODE"** (Autonomous File & Terminal Writing)
* **Best Use Cases:**
  * Generating complete, standalone boilerplate configuration files (e.g., `docker-compose.yml`, Terraform scripts).
  * Writing complex mathematical, algorithmic, or heavy logical backend functions.
  * Building clean, raw frontend components based on strict specifications.
  * *Why:* Highest "first-time right" syntax rate, minimizing the need for expensive debugging cycles.

---

## 🛠️ Phase 3: Modifying & Refactoring Existing Code
* **Best Model:** `kimi-k2.7-code`
* **Cline Mode:** **"ACT MODE"** (Autonomous File & Terminal Writing)
* **Best Use Cases:**
  * Injecting a brand new feature into an already crowded script or file.
  * Modifying existing lines of code using Cline's search-and-replace ("Diff") tools.
  * Refactoring old legacy code to optimize performance or readability.
  * Connecting multiple files, routes, or APIs together across different folders.
  * *Why:* It has 30% less internal "thinking token" bloat and holds the #1 spot globally for reliable tool-calling and file manipulation.

---

## 🚨 Phase 4: Emergency Catastrophic Debugging
* **Best Model:** `glm-5.2`
* **Cline Mode:** **"ACT MODE"** (Autonomous File & Terminal Writing)
* **Best Use Cases:**
  * Resolving a severe server crash or a deep compilation nightmare you can't figure out yourself.
  * Diagnosing runtime or environmental bugs where the terminal output is throwing repeated errors.
  * Navigating across massive, multi-folder legacy repositories to find a silent dependency bug.
  * *Why:* Elite long-horizon stamina and an unmatched 1-Million Token context window specifically trained to trace looping terminal exceptions.

---

## 🛟 Safe-Fail Prompt Guardrail
Paste this block into your Cline **Custom Instructions/System Prompt** to stop any model from silently draining your monthly quota:

"If any file edit or terminal execution fails 2 times consecutively, STOP immediately. Do not attempt a third automated fix. Report the exact error to the user and await manual instructions."
