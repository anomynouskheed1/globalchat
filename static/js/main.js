/* ===== THEME ===== */
const themeToggle = document.getElementById('themeToggle');
const savedTheme = localStorage.getItem('theme') || 'light';
document.documentElement.setAttribute('data-theme', savedTheme);
if (themeToggle) {
  themeToggle.textContent = savedTheme === 'dark' ? '☀️' : '🌙';
  themeToggle.addEventListener('click', () => {
    const current = document.documentElement.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
    themeToggle.textContent = next === 'dark' ? '☀️' : '🌙';
  });
}

/* ===== NAVBAR SCROLL ===== */
const navbar = document.getElementById('navbar');
window.addEventListener('scroll', () => {
  if (navbar) navbar.classList.toggle('scrolled', window.scrollY > 20);
});

/* ===== PARALLAX ===== */
const parallaxBg = document.getElementById('parallaxBg');
window.addEventListener('scroll', () => {
  if (parallaxBg) {
    parallaxBg.style.transform = `translateY(${window.scrollY * 0.4}px)`;
  }
});

/* ===== REVEAL ON SCROLL ===== */
const revealObserver = new IntersectionObserver((entries) => {
  entries.forEach((entry, i) => {
    if (entry.isIntersecting) {
      setTimeout(() => entry.target.classList.add('visible'), i * 80);
      revealObserver.unobserve(entry.target);
    }
  });
}, { threshold: 0.12 });

document.querySelectorAll('.reveal, .reveal-right').forEach(el => revealObserver.observe(el));

/* ===== HAMBURGER ===== */
const hamburger = document.getElementById('hamburger');
if (hamburger) {
  hamburger.addEventListener('click', () => {
    const links = document.querySelector('.nav-links');
    if (links) {
      links.style.display = links.style.display === 'flex' ? 'none' : 'flex';
      links.style.flexDirection = 'column';
      links.style.position = 'absolute';
      links.style.top = '64px';
      links.style.left = '0';
      links.style.right = '0';
      links.style.background = 'var(--bg)';
      links.style.padding = '1rem 1.5rem';
      links.style.borderBottom = '1px solid var(--border)';
      links.style.zIndex = '999';
    }
  });
}

/* ===== AUTH TABS ===== */
function switchTab(id, btn) {
  document.querySelectorAll('.auth-form').forEach(f => f.classList.add('hidden'));
  document.querySelectorAll('.auth-tab').forEach(t => t.classList.remove('active'));
  const target = document.getElementById(id);
  if (target) target.classList.remove('hidden');
  if (btn) btn.classList.add('active');
}

function handleAuth(e) {
  e.preventDefault();
  showNotification('✅ Success! Redirecting...');
  setTimeout(() => location.href = '/dashboard', 1200);
}

/* ===== PAYMENT TABS ===== */
function switchPay(id, btn) {
  const parent = btn.closest('.modal') || document.body;
  parent.querySelectorAll('.pay-form').forEach(f => f.classList.add('hidden'));
  parent.querySelectorAll('.pay-tab').forEach(t => t.classList.remove('active'));
  const target = document.getElementById(id);
  if (target) target.classList.remove('hidden');
  if (btn) btn.classList.add('active');
}

/* ===== MEMBERSHIP MODAL ===== */
function showPayment() {
  const modal = document.getElementById('paymentModal');
  if (modal) modal.classList.remove('hidden');
}
function closePayment() {
  const modal = document.getElementById('paymentModal');
  if (modal) modal.classList.add('hidden');
}
function simulatePayment() {
  closePayment();
  setTimeout(() => {
    const success = document.getElementById('successModal');
    if (success) success.classList.remove('hidden');
  }, 400);
}

/* ===== WALLET MODAL ===== */
function showWithdraw() {
  const modal = document.getElementById('withdrawModal');
  if (modal) modal.classList.remove('hidden');
}
function closeWithdraw() {
  const modal = document.getElementById('withdrawModal');
  if (modal) modal.classList.add('hidden');
}
function simulateWithdraw() {
  closeWithdraw();
  setTimeout(() => {
    const success = document.getElementById('withdrawSuccess');
    if (success) success.classList.remove('hidden');
  }, 400);
}
function closeWithdrawSuccess() {
  const modal = document.getElementById('withdrawSuccess');
  if (modal) modal.classList.add('hidden');
}

/* ===== CLOSE MODALS ON OVERLAY CLICK ===== */
document.querySelectorAll('.modal-overlay').forEach(overlay => {
  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) overlay.classList.add('hidden');
  });
});

/* ===== FAQ ===== */
function toggleFaq(btn) {
  const answer = btn.nextElementSibling;
  const isOpen = answer.classList.contains('open');
  document.querySelectorAll('.faq-a').forEach(a => a.classList.remove('open'));
  document.querySelectorAll('.faq-q').forEach(q => q.classList.remove('open'));
  if (!isOpen) {
    answer.classList.add('open');
    btn.classList.add('open');
  }
}

/* ===== SURVEY (15 questions) ===== */
let currentSQ = 1;
const totalSQ = 15;
let surveyRating = 0;

function updateSurveyProgress() {
  const pct = Math.round((currentSQ / totalSQ) * 100);
  const fill = document.getElementById('spFill');
  const num = document.getElementById('sqNum');
  const pctEl = document.getElementById('spPct');
  if (fill) fill.style.width = pct + '%';
  if (num) num.textContent = currentSQ;
  if (pctEl) pctEl.textContent = pct + '%';
  const prev = document.getElementById('sqPrev');
  const next = document.getElementById('sqNext');
  if (prev) prev.disabled = currentSQ === 1;
  if (next) next.textContent = currentSQ === totalSQ ? 'Submit ✓' : 'Next →';
}

function nextSQ() {
  const card = document.querySelector(`.sq-card[data-sq="${currentSQ}"]`);
  if (!card) return;
  const selected = card.querySelector('.q-opt.selected');
  const textarea = card.querySelector('.survey-textarea');
  const ratingDone = card.querySelector('.star-btn.active');
  if (!selected && !textarea && !ratingDone && currentSQ !== 15) {
    showNotification('⚠️ Please select an answer'); return;
  }
  if (currentSQ === totalSQ) {
    card.classList.add('hidden');
    const nav = document.getElementById('sqNav');
    if (nav) nav.classList.add('hidden');
    const complete = document.getElementById('surveyComplete');
    if (complete) { complete.classList.remove('hidden'); setTimeout(() => complete.classList.add('visible'), 50); }
    return;
  }
  card.classList.add('hidden');
  currentSQ++;
  const next = document.querySelector(`.sq-card[data-sq="${currentSQ}"]`);
  if (next) { next.classList.remove('hidden'); setTimeout(() => next.classList.add('visible'), 50); }
  updateSurveyProgress();
}

function prevSQ() {
  if (currentSQ <= 1) return;
  const card = document.querySelector(`.sq-card[data-sq="${currentSQ}"]`);
  if (card) card.classList.add('hidden');
  currentSQ--;
  const prev = document.querySelector(`.sq-card[data-sq="${currentSQ}"]`);
  if (prev) { prev.classList.remove('hidden'); setTimeout(() => prev.classList.add('visible'), 50); }
  updateSurveyProgress();
}

function rateStar(n) {
  surveyRating = n;
  const labels = ['', 'Poor', 'Fair', 'Good', 'Very Good', 'Excellent'];
  const stars = document.querySelectorAll('.star-btn');
  stars.forEach((s, i) => { s.classList.toggle('active', i < n); });
  const label = document.getElementById('ratingLabel');
  if (label) label.textContent = labels[n] + ' (' + n + '/5)';
}

/* ===== SCREENING ===== */
let currentQuestion = 1;
const totalQuestions = 5;

function updateProgress() {
  const pct = Math.round((currentQuestion / totalQuestions) * 100);
  const fill = document.getElementById('progressFill');
  const cur = document.getElementById('currentQ');
  const pctEl = document.getElementById('progressPct');
  if (fill) fill.style.width = pct + '%';
  if (cur) cur.textContent = currentQuestion;
  if (pctEl) pctEl.textContent = pct + '%';
  const prevBtn = document.getElementById('prevBtn');
  const nextBtn = document.getElementById('nextBtn');
  if (prevBtn) prevBtn.disabled = currentQuestion === 1;
  if (nextBtn) nextBtn.textContent = currentQuestion === totalQuestions ? 'Submit ✓' : 'Next →';
}

function selectOpt(btn) {
  const card = btn.closest('.q-card');
  card.querySelectorAll('.q-opt').forEach(o => o.classList.remove('selected'));
  btn.classList.add('selected');
}

function nextQ() {
  const card = document.querySelector(`.q-card[data-q="${currentQuestion}"]`);
  if (!card) return;
  const selected = card.querySelector('.q-opt.selected');
  if (!selected) { showNotification('⚠️ Please select an answer'); return; }

  if (currentQuestion === totalQuestions) {
    card.classList.add('hidden');
    const completion = document.getElementById('completionCard');
    if (completion) {
      completion.classList.remove('hidden');
      setTimeout(() => completion.classList.add('visible'), 50);
    }
    document.querySelector('.q-nav').classList.add('hidden');
    document.querySelector('.progress-bar-wrap').classList.add('hidden');
    return;
  }

  card.classList.add('hidden');
  currentQuestion++;
  const next = document.querySelector(`.q-card[data-q="${currentQuestion}"]`);
  if (next) {
    next.classList.remove('hidden');
    setTimeout(() => next.classList.add('visible'), 50);
  }
  updateProgress();
}

function prevQ() {
  if (currentQuestion <= 1) return;
  const card = document.querySelector(`.q-card[data-q="${currentQuestion}"]`);
  if (card) card.classList.add('hidden');
  currentQuestion--;
  const prev = document.querySelector(`.q-card[data-q="${currentQuestion}"]`);
  if (prev) {
    prev.classList.remove('hidden');
    setTimeout(() => prev.classList.add('visible'), 50);
  }
  updateProgress();
}

/* ===== TX FILTER ===== */
function filterTx(type, btn) {
  document.querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
  if (btn) btn.classList.add('active');
  document.querySelectorAll('.tx-item').forEach(item => {
    item.style.display = (type === 'all' || item.dataset.type === type) ? 'flex' : 'none';
  });
}

/* ===== REFERRAL COPY ===== */
function copyRef() {
  const input = document.getElementById('refLink');
  if (input) {
    navigator.clipboard.writeText(input.value).then(() => showNotification('🔗 Referral link copied!'));
  }
}

/* ===== NOTIFICATION ===== */
function showNotification(msg) {
  let notif = document.querySelector('.notification');
  if (!notif) {
    notif = document.createElement('div');
    notif.className = 'notification';
    document.body.appendChild(notif);
  }
  notif.textContent = msg;
  notif.classList.add('show');
  setTimeout(() => notif.classList.remove('show'), 3000);
}

/* ===== TASK FILTER ===== */
function filterTasks(type, btn) {
  document.querySelectorAll('.filter-pill').forEach(b => b.classList.remove('active'));
  if (btn) btn.classList.add('active');
  document.querySelectorAll('.task-card').forEach(card => {
    card.style.display = (type === 'all' || card.dataset.type === type) ? '' : 'none';
  });
}

function sortTasks(val) {
  showNotification('🔄 Sorting tasks by: ' + val);
}

/* ===== LEADERBOARD TABS ===== */
function switchLbTab(id, btn) {
  document.querySelectorAll('.lb-tab').forEach(t => t.classList.remove('active'));
  if (btn) btn.classList.add('active');
  showNotification('📊 Showing ' + id + ' rankings');
}

/* ===== CHAT ===== */
let chatMsgCount = 3;
const chatGoal = 10;

function sendMessage() {
  const input = document.getElementById('chatInput');
  if (!input || !input.value.trim()) return;
  const messages = document.getElementById('chatMessages');
  if (!messages) return;

  const row = document.createElement('div');
  row.className = 'msg-row me';
  row.innerHTML = `<div class="msg-content">
    <div class="msg-bubble">${input.value.trim()}</div>
    <span class="msg-time">Just now</span>
  </div>`;
  const typing = messages.querySelector('.typing-indicator');
  if (typing) messages.insertBefore(row, typing);
  else messages.appendChild(row);
  messages.scrollTop = messages.scrollHeight;
  input.value = '';

  chatMsgCount++;
  const pct = Math.min((chatMsgCount / chatGoal) * 100, 100);
  const bar = document.getElementById('chatProgress');
  const label = document.getElementById('chatProgressLabel');
  if (bar) bar.style.width = pct + '%';
  if (label) label.textContent = Math.min(chatMsgCount, chatGoal) + ' / ' + chatGoal + ' messages';

  if (chatMsgCount >= chatGoal) {
    showNotification('🎉 Chat task complete! KES 100 added to your wallet');
  }

  // Simulate a reply after 1.5s
  setTimeout(() => {
    const replies = [
      "That's a great point! 👍",
      "Totally agree with you on that 🔥",
      "Interesting perspective! Thanks for sharing.",
      "Has anyone else experienced this? 🤔",
      "Good to know, I'll try that out!",
    ];
    const reply = document.createElement('div');
    reply.className = 'msg-row other';
    reply.innerHTML = `<div class="msg-avatar">JM</div>
    <div class="msg-content">
      <span class="msg-name">James M.</span>
      <div class="msg-bubble">${replies[Math.floor(Math.random() * replies.length)]}</div>
      <span class="msg-time">Just now</span>
    </div>`;
    const t = messages.querySelector('.typing-indicator');
    if (t) messages.insertBefore(reply, t);
    else messages.appendChild(reply);
    messages.scrollTop = messages.scrollHeight;
  }, 1500);
}

function switchRoom(name, btn) {
  document.querySelectorAll('.room-item').forEach(r => r.classList.remove('active'));
  if (btn) btn.classList.add('active');
  const title = document.getElementById('roomTitle');
  if (title) title.textContent = name;
  showNotification('💬 Joined ' + name);
}

function toggleEmoji() {
  const picker = document.getElementById('emojiPicker');
  if (picker) picker.classList.toggle('hidden');
}

function addEmoji(e) {
  const input = document.getElementById('chatInput');
  if (input) input.value += e;
  toggleEmoji();
  input.focus();
}

/* ===== PROFILE ===== */
function toggleEditProfile() {
  const card = document.getElementById('editProfileCard');
  if (card) card.classList.toggle('hidden');
}
