export const serviceColors = {
  ec2: '#3b82f6',
  rds: '#8b5cf6',
  ebs: '#06b6d4',
  lambda: '#f59e0b',
  s3: '#ec4899',
  compute: '#3b82f6',
  cloudsql: '#8b5cf6',
  'persistent-disk': '#06b6d4',
  functions: '#f59e0b',
  vm: '#0ea5e9',
  sql: '#a855f7',
  'managed-disk': '#14b8a6',
  other: '#64748b',
}

export function colorForService(service) {
  return serviceColors[service] ?? '#64748b'
}

export function formatCurrency(value, { maximumFractionDigits = 0 } = {}) {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits,
  }).format(value ?? 0)
}
