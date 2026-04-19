import DashboardLayout from './components/DashboardLayout'
import { RefreshProvider } from './context/RefreshContext'

export default function App() {
  return (
    <RefreshProvider>
      <DashboardLayout />
    </RefreshProvider>
  )
}
