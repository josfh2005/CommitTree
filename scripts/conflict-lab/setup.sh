#!/usr/bin/env bash
# Builds (or rebuilds from scratch) a repository whose merge of
# feature/checkout into develop conflicts in the ways that trip AI
# resolvers up. Run it before each model you try, then grade with check.py.
#
#   scripts/conflict-lab/setup.sh [dir]     (default ~/playground/conflict-lab)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DIR="${1:-$HOME/playground/conflict-lab}"
if [ -e "$DIR" ] && [ ! -f "$DIR/.conflict-lab" ]; then
  echo "$DIR exists and is not a conflict lab; refusing to delete it" >&2
  exit 1
fi
rm -rf "$DIR"
mkdir -p "$DIR"
cd "$DIR"
git init -q -b main
git config user.name "Conflict Lab"
git config user.email "lab@example.com"
git config merge.conflictStyle diff3
touch .conflict-lab
printf '.conflict-lab\n' > .gitignore

w() { mkdir -p "$(dirname "$1")"; cat > "$1"; }
commit() { git add -A && git commit -q -m "$1"; }

# ---------------------------------------------------------------- base
w src/app/chat-window.component.html <<'EOF'
<div class="chat">
  <app-header [title]="title"></app-header>
  <app-messages [items]="messages"></app-messages>
</div>

<esc-simple-action-form #simpleActionForm></esc-simple-action-form>
EOF
w src/app/orders.service.ts <<'EOF'
import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class OrdersService {
  private readonly baseUrl = '/api/orders';

  constructor(private http: HttpClient) {}

  list(page: number) {
    return this.http.get(`${this.baseUrl}?page=${page}`);
  }

  get(id: string) {
    return this.http.get(`${this.baseUrl}/${id}`);
  }

  remove(id: string) {
    return this.http.delete(`${this.baseUrl}/${id}`);
  }
}
EOF
w src/utils/format.ts <<'EOF'
export function formatPrice(amount: number): string {
  return amount.toFixed(2) + ' EUR';
}
EOF
w src/app/cart.ts <<'EOF'
import { formatPrice } from '../utils/format';

export function cartTotalLabel(total: number): string {
  return 'Total: ' + formatPrice(total);
}
EOF
w config/settings.json <<'EOF'
{
  "features": {
    "darkMode": true
  },
  "retries": 3,
  "logLevel": "info",
  "apiTimeoutMs": 30000
}
EOF
w src/billing/tax.py <<'EOF'
def tax_rate(country: str) -> float:
    if country == "ES":
        return 0.21
    elif country == "PT":
        return 0.23
    return 0.0
EOF
w src/billing/discount.py <<'EOF'
def member_discount(total: float) -> float:
    return total * 0.05


def legacy_discount(total: float) -> float:
    # Kept for the old checkout.
    return total * 0.10
EOF
w src/legacy/old-report.ts <<'EOF'
export function oldReport(rows: string[]): string {
  return rows.join('\n');
}
EOF
w README.md <<'EOF'
# Shop

## Setup
- Install Node 22
- Run npm ci
EOF
commit "Initial shop"
git branch develop
git switch -q -c feature/checkout

# ------------------------------------------------ theirs: feature/checkout
cat >> src/app/chat-window.component.html <<'EOF'

<app-record-manage-form #contactRecordForm></app-record-manage-form>
EOF
w src/app/orders.service.ts <<'EOF'
import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { retry } from 'rxjs/operators';

@Injectable({ providedIn: 'root' })
export class OrdersService {
  private readonly baseUrl = '/api/orders';

  constructor(private http: HttpClient) {}

  list(page: number) {
    return this.http.get(`${this.baseUrl}?page=${page}`).pipe(retry(2));
  }

  get(id: string) {
    return this.http.get(`${this.baseUrl}/${id}`);
  }

  remove(id: string) {
    return this.http.delete(`${this.baseUrl}/${id}`);
  }

  export(format: 'csv' | 'xlsx') {
    return this.http.get(`${this.baseUrl}/export?format=${format}`, { responseType: 'blob' });
  }
}
EOF
w src/utils/format.ts <<'EOF'
export function formatPrice(amount: number, currency = 'EUR'): string {
  return new Intl.NumberFormat('es-ES', { style: 'currency', currency }).format(amount);
}
EOF
w config/settings.json <<'EOF'
{
  "features": {
    "darkMode": true,
    "exports": true
  },
  "retries": 3,
  "logLevel": "info",
  "apiTimeoutMs": 60000
}
EOF
w src/billing/tax.py <<'EOF'
def tax_rate(country: str) -> float:
    if country == "ES":
        return 0.21
    elif country == "PT":
        return 0.23
    elif country == "IT":
        return 0.22
    return 0.0
EOF
w src/billing/discount.py <<'EOF'
def member_discount(total: float) -> float:
    return total * 0.05


def legacy_discount(total: float) -> float:
    # Kept for the old checkout; capped since the 2026 promo.
    return min(total * 0.10, 50.0)
EOF
w src/legacy/old-report.ts <<'EOF'
export function oldReport(rows: string[]): string {
  return rows.filter(Boolean).join('\n');
}
EOF
w README.md <<'EOF'
# Shop

## Setup
- Install Node 22
- Run npm ci
- Copy .env.example to .env
- Run docker compose up -d
EOF
commit "Checkout: exports, retries, currency formatting, Italy"

# ----------------------------------------------------------- ours: develop
git switch -q develop
cat >> src/app/chat-window.component.html <<'EOF'

<ng-template #chatOptionsMenu>
  <clr-dropdown>
    <esc-icon-button iconName="ellipsis-vertical" clrDropdownTrigger></esc-icon-button>
    <clr-dropdown-menu clrPosition="bottom-right">
      <div (click)="onAssignConversation()" class="drop-item" clrDropdownItem>
        <clr-icon shape="user"></clr-icon>
        <span>{{ "shared.forms.labels.assignConversation" | translate }}</span>
      </div>
      <div *ngIf="conversationOpen" (click)="onCloseConversation()" class="drop-item" clrDropdownItem>
        <clr-icon shape="logout"></clr-icon>
        <span>{{ "inbox.conversation.close.help" | translate }}</span>
      </div>
    </clr-dropdown-menu>
  </clr-dropdown>
</ng-template>
EOF
w src/app/orders.service.ts <<'EOF'
import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { Order } from './order.model';

@Injectable({ providedIn: 'root' })
export class OrdersService {
  private readonly baseUrl = '/api/orders';

  constructor(private http: HttpClient) {}

  list(page: number, size = 20): Observable<Order[]> {
    return this.http.get<Order[]>(`${this.baseUrl}?page=${page}&size=${size}`);
  }

  get(id: string) {
    return this.http.get(`${this.baseUrl}/${id}`);
  }

  remove(id: string) {
    return this.http.delete(`${this.baseUrl}/${id}`);
  }

  cancel(id: string) {
    return this.http.post(`${this.baseUrl}/${id}/cancel`, {});
  }
}
EOF
w src/app/order.model.ts <<'EOF'
export interface Order {
  id: string;
  total: number;
}
EOF
w src/utils/format.ts <<'EOF'
export function formatMoney(amount: number): string {
  return amount.toFixed(2) + ' EUR';
}
EOF
w src/app/cart.ts <<'EOF'
import { formatMoney } from '../utils/format';

export function cartTotalLabel(total: number): string {
  return 'Total: ' + formatMoney(total);
}
EOF
w config/settings.json <<'EOF'
{
  "features": {
    "darkMode": true,
    "inbox": true
  },
  "retries": 3,
  "logLevel": "info",
  "apiTimeoutMs": 45000
}
EOF
w src/billing/tax.py <<'EOF'
def tax_rate(country: str) -> float:
    if country == "ES":
        return 0.21
    elif country == "PT":
        return 0.23
    elif country == "FR":
        return 0.20
    return 0.0
EOF
w src/billing/discount.py <<'EOF'
def member_discount(total: float) -> float:
    return total * 0.05
EOF
git rm -q src/legacy/old-report.ts
w README.md <<'EOF'
# Shop

## Setup
- Install Node 22
- Run npm ci
- Copy .env.example to .env
- Run npm run dev
EOF
commit "Develop: typed orders, inbox flag, France, drop legacy code"

echo "Conflict lab ready in $DIR (on develop)."
echo "In CommitTree: add the repository, merge feature/checkout into develop,"
echo "resolve with AI, then (before committing):"
echo "  python3 $HERE/check.py $DIR --model <model-name>"
