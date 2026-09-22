import { Inter } from 'next/font/google';
import { Provider } from '@/components/provider';
import type { Metadata } from 'next';
import './global.css';

const inter = Inter({
  subsets: ['latin'],
});

export const metadata: Metadata = {
  title: {
    default: 'wArrden Documentation',
    template: '%s | wArrden',
  },
  description:
    'Install and configure wArrden to search for missing or upgradeable media and clean stuck arr queue entries.',
  icons: {
    icon: '/logo.png',
  },
};

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="en" className={`${inter.className} dark`} style={{ colorScheme: 'dark' }}>
      <body className="flex flex-col min-h-screen">
        <Provider>{children}</Provider>
      </body>
    </html>
  );
}
