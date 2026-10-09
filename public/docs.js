const search=document.querySelector('#docs-search');
try{
  const route=sessionStorage.getItem('distvis-docs-return');
  if(/^#(?:library|protocol\/[a-f0-9-]+\/(?:code|experiments)|experiment\/[a-f0-9-]+\/(?:code|visual|history))$/.test(route || '')){
    document.querySelector('.back-link').href='/'+route;
  }
}catch{}
const sections=[...document.querySelectorAll('.doc-section')];
function filter(){
  const query=search.value.trim().toLowerCase();let count=0;
  for(const section of sections){
    const match=!query || section.textContent.toLowerCase().includes(query);
    section.hidden=!match;if(match)count++;
    const link=[...document.querySelectorAll('[data-section-link]')].find(a=>a.dataset.sectionLink===section.dataset.section);
    if(link)link.hidden=!match;
  }
  document.querySelector('#docs-no-results').hidden=count>0;
}
search.addEventListener('input',filter);
const toc=document.querySelector('#docs-toc');
if(matchMedia('(max-width: 760px)').matches)toc.open=false;
for(const button of document.querySelectorAll('.copy-code'))button.addEventListener('click',async()=>{
  try{await navigator.clipboard.writeText(button.closest('.code-block').querySelector('code').textContent);button.textContent='已复制';setTimeout(()=>button.textContent='复制',1500);}
  catch{document.querySelector('#docs-status').textContent='无法自动复制，请选中代码手动复制。';}
});
function revealHash(){
  let id;try{id=decodeURIComponent(location.hash.slice(1));}catch{return;}
  const target=document.getElementById(id);if(!target)return;
  if(target.closest('.doc-section')?.hidden){search.value='';filter();}
  target.scrollIntoView();
}
window.addEventListener('hashchange',revealHash);
revealHash();
